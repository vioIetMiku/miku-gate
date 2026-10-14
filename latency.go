package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GateLatency is measured on this VPS, never taken from VPN Gate's list Ping.
type GateLatency struct {
	MS      *float64  `json:"ms,omitempty"`
	Target  string    `json:"target"`
	Method  string    `json:"method"`
	Checked time.Time `json:"checked"`
	Pending bool      `json:"pending,omitempty"`
	Error   string    `json:"error,omitempty"`
	Port    int       `json:"port,omitempty"`
}

const latencyInterval = 30 * time.Second

type LatencyInspector struct {
	mu            sync.Mutex
	cache         map[string]GateLatency
	pending       map[string]bool
	queue         chan string
	probe         func(string) (float64, error)
	tcpProbe      func(context.Context, gateEndpoint) (float64, error)
	profiles      map[string]Node
	icmpBlocked   map[string]time.Time
	tunnelSamples map[string]GateLatency
}

func newLatencyInspector() *LatencyInspector {
	s := &LatencyInspector{cache: map[string]GateLatency{}, pending: map[string]bool{}, queue: make(chan string, 1024), probe: pingGate, tcpProbe: tcpEndpointLatency, profiles: map[string]Node{}, icmpBlocked: map[string]time.Time{}, tunnelSamples: map[string]GateLatency{}}
	for i := 0; i < 8; i++ {
		go func() {
			for target := range s.queue {
				s.measure(target)
			}
		}()
	}
	return s
}

func gatePingTarget(node Node) string {
	// Prefer the active OpenVPN profile's endpoint over list metadata.
	// ExitIP is a traffic egress address and is never used as a ping target.
	for _, line := range strings.Split(node.Config, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "remote" {
			if ip, err := publicLookupIP(strings.Trim(parts[1], "\"'")); err == nil {
				return ip
			}
		}
	}
	if ip, err := publicLookupIP(node.IP); err == nil {
		return ip
	}
	return ""
}

func (s *LatencyInspector) get(node Node) *GateLatency {
	target := gatePingTarget(node)
	if target == "" {
		return &GateLatency{Error: "VPN Gate服务端地址不可用"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := latencyKey(node)
	if s.profiles == nil {
		s.profiles = map[string]Node{}
	}
	s.profiles[key] = node
	value, ok := s.cache[key]
	if !ok || time.Since(value.Checked) >= latencyInterval {
		if !s.pending[key] {
			select {
			case s.queue <- key:
				s.pending[key] = true
			default:
			}
		}
	}
	if value.Target == "" {
		value.Target = target
	}
	value.Pending = s.pending[key]
	if value.MS == nil || value.Error != "" || time.Since(value.Checked) > 90*time.Second {
		if sample, exists := s.tunnelSamples[key]; exists && time.Since(sample.Checked) < 45*time.Second {
			sample.Pending = value.Pending
			value = sample
		}
	}
	if !ok && !value.Pending {
		if value.MS == nil {
			value.Error = "延迟检测排队中"
		}
	}
	// Never sum a long-expired sample after a failed or blocked refresh.
	if time.Since(value.Checked) > 90*time.Second {
		value.MS = nil
	}
	return &value
}

func (s *LatencyInspector) measure(key string) {
	s.mu.Lock()
	node, ok := s.profiles[key]
	if !ok {
		node = Node{IP: key}
	}
	target := gatePingTarget(node)
	blocked := time.Now().Before(s.icmpBlocked[target])
	s.mu.Unlock()
	var ms float64
	err := fmt.Errorf("ICMP未响应")
	value := GateLatency{Target: target, Method: "icmp"}
	if !blocked {
		ms, err = s.probe(target)
	}
	if err != nil {
		s.mu.Lock()
		if s.icmpBlocked == nil {
			s.icmpBlocked = map[string]time.Time{}
		}
		if !blocked {
			s.icmpBlocked[target] = time.Now().Add(5 * time.Minute)
		}
		s.mu.Unlock()
		probe := s.tcpProbe
		if probe == nil {
			probe = tcpEndpointLatency
		}
		for _, ep := range nodeProbeEndpoints(node) {
			if !strings.HasPrefix(ep.Proto, "tcp") {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			ms, err = probe(ctx, ep)
			cancel()
			if err == nil {
				value.Method, value.Target, value.Port = "tcp", ep.IP, ep.Port
				break
			}
		}
	}
	value.Checked = time.Now()
	if err != nil {
		value.Error = "ICMP及配置中的TCP端点暂无测量结果，等待隧道检测"
	} else if ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		value.Error = "VPN Gate ping响应无效"
	} else {
		value.MS = &ms
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = value
	delete(s.pending, key)
	if len(s.cache) > 2048 {
		for oldKey, old := range s.cache {
			if oldKey != key && !s.pending[oldKey] && time.Since(old.Checked) > 90*time.Second {
				delete(s.cache, oldKey)
				delete(s.profiles, oldKey)
				delete(s.tunnelSamples, oldKey)
			}
		}
	}
	if len(s.icmpBlocked) > 2048 {
		for ip, until := range s.icmpBlocked {
			if time.Now().After(until) {
				delete(s.icmpBlocked, ip)
			}
		}
	}
}

func (s *LatencyInspector) recordTunnelSample(node Node, ms float64, target string) {
	s.recordTunnelSampleAt(node, ms, target, time.Now())
}

func (s *LatencyInspector) recordTunnelSampleAt(node Node, ms float64, target string, now time.Time) {
	if ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tunnelSamples == nil {
		s.tunnelSamples = map[string]GateLatency{}
	}
	key := latencyKey(node)
	if old, ok := s.tunnelSamples[key]; ok && now.Sub(old.Checked) < latencyInterval {
		return
	}
	s.tunnelSamples[key] = GateLatency{MS: &ms, Target: target, Method: "tunnel_tcp", Checked: now}
}

var pingAverage = regexp.MustCompile(`(?:rtt|round-trip)[^\n=]*=\s*[0-9.]+/([0-9.]+)/[0-9.]+`)

func parsePingAverage(output string) (float64, error) {
	match := pingAverage.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0, fmt.Errorf("VPN Gate ping未返回往返时间")
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("VPN Gate ping响应无效")
	}
	return value, nil
}

func pingGate(target string) (float64, error) {
	return pingGateContext(context.Background(), target)
}

func pingGateContext(parent context.Context, target string) (float64, error) {
	if _, err := publicLookupIP(target); err != nil {
		return 0, fmt.Errorf("VPN Gate地址无效")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	args := []string{"-n", "-c", "2", "-i", "0.2", "-W", "1", "-w", "2"}
	if net.ParseIP(target).To4() == nil {
		args = append(args, "-6")
	}
	args = append(args, target)
	cmd := exec.CommandContext(ctx, "ping", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	// cmdOutput explicitly starts in the original host network namespace.
	out, err := cmdOutput(cmd)
	if err != nil {
		if ctx.Err() != nil {
			return 0, fmt.Errorf("VPN Gate ping超时")
		}
		if _, missing := exec.LookPath("ping"); missing != nil {
			return 0, fmt.Errorf("VPS缺少ping命令，请安装iputils-ping")
		}
		return 0, fmt.Errorf("VPN Gate未响应ICMP ping")
	}
	return parsePingAverage(string(out))
}

// Kept behind the same login and base path as the rest of the management API.
func apiBrowserPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const latencyJS = `
const BROWSER_LATENCY_INTERVAL=30000;
const browserLatencyKey='miku.browser-latency.'+(location.pathname || '/');
let clientLatencyMS=null, clientLatencyChecked=0, browserPingBusy=false, browserPingError='', browserPingAttempted=0;
try{
  const saved=JSON.parse(sessionStorage.getItem(browserLatencyKey) || 'null'),now=Date.now();
  if(saved && Number.isFinite(saved.attempted) && saved.attempted>0 && saved.attempted<=now){
    browserPingAttempted=saved.attempted;
    if(Number.isFinite(saved.ms) && saved.ms>=0 && Number.isFinite(saved.checked) && saved.checked>0 && saved.checked<=now && now-saved.checked<60000){
      clientLatencyMS=saved.ms;clientLatencyChecked=saved.checked;
    }
    if(now-saved.attempted<BROWSER_LATENCY_INTERVAL && saved.error===true) browserPingError='本地到VPS测量失败，稍后自动重试';
  }
}catch(e){}
function saveBrowserLatency(){
  try{sessionStorage.setItem(browserLatencyKey,JSON.stringify({attempted:browserPingAttempted,ms:clientLatencyMS,checked:clientLatencyChecked,error:!!browserPingError}));}catch(e){}
}
function latencyOf(exit){
  const gate=exit.gate_latency;
  const method=gate && ({icmp:'ICMP',tcp:'TCP',tunnel_tcp:'隧道 TCP'})[gate.method] || '';
  const approx=!!gate && gate.method==='tunnel_tcp';
  const explanation=approx?'参考总延迟 = 本地→VPS 浏览器往返时间 + 经VPN隧道到检测站的TCP时间，包含出口到检测站开销':'总延迟 = 本地→VPS 浏览器往返时间 + VPS→VPN Gate '+(method || '网络')+' 往返时间；ICMP不响应时自动改测配置中的TCP端口';
  if(exit.status!=='up') return {label:'—',title:explanation,parts:''};
  if(!gate || gate.ms==null || !Number.isFinite(gate.ms) || gate.ms<0 || !Number.isFinite(Date.parse(gate.checked)) || Date.now()-Date.parse(gate.checked)>90000){
    return {label:gate && gate.error && !gate.pending?'暂未测得':'测量中…',title:gate && gate.error?gate.error+'；'+explanation:explanation,parts:'',method:'',approx:false};
  }
  if(clientLatencyMS==null || Date.now()-clientLatencyChecked>60000){
    return {label:browserPingError?'本地测量失败':'测量中…',title:browserPingError || explanation,parts:''};
  }
  const total=clientLatencyMS+gate.ms;
  const parts='本地 '+clientLatencyMS.toFixed(1)+' + '+(approx?'隧道':'Gate')+' '+gate.ms.toFixed(1)+' ms';
  return {label:Math.round(total)+' ms',title:parts+'；'+explanation,parts:parts,method:method,approx:approx};
}
async function measureBrowserPing(){
  const now=Date.now();
  if(browserPingBusy || document.hidden || (browserPingAttempted && now-browserPingAttempted<BROWSER_LATENCY_INTERVAL)) return;
  browserPingAttempted=now;
  saveBrowserLatency();
  browserPingBusy=true;
  const samples=[];
  try{
    // Warm the same-origin connection, then use the median of three round trips.
    for(let i=0;i<4;i++){
      const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),8000);
      try{
        const started=performance.now();
        const response=await fetch('api/ping?_='+Date.now()+'-'+i,{cache:'no-store',credentials:'same-origin',signal:controller.signal});
        await response.arrayBuffer();
        if(response.status!==204) throw new Error('本地到VPS测量未完成，请检查登录和网络');
        const ms=performance.now()-started;
        if(i>0 && Number.isFinite(ms) && ms>=0) samples.push(ms);
      }finally{clearTimeout(timer);}
    }
    if(samples.length<2) throw new Error('本地到VPS测量失败，稍后自动重试');
    samples.sort((a,b)=>a-b);
    clientLatencyMS=samples[Math.floor(samples.length/2)];
    clientLatencyChecked=Date.now();browserPingError='';
  }catch(err){clientLatencyMS=null;browserPingError='本地到VPS测量失败，稍后自动重试';}
  finally{browserPingBusy=false;saveBrowserLatency();renderExits();}
}
document.addEventListener('visibilitychange',()=>{if(!document.hidden) measureBrowserPing();});
`
