package main

import (
	"log"
	"time"
)

const (
	healthInterval = 10 * time.Second
	healthFailures = 2 // 连续失败几次才判定掉线，避免网络抖动误杀
)

// WatchHealth 周期检查每条隧道是否还能出网，掉线的自动换节点重连。
// VPN Gate 是志愿者节点，运行中掉线很常见。
func (m *Manager) WatchHealth() {
	fails := map[*Tunnel]int{}
	pending := map[*Tunnel]bool{}
	queue := make(chan healthTask, 1024)
	results := make(chan healthResult, 1024)
	for i := 0; i < 8; i++ {
		go func() {
			for task := range queue {
				results <- probeTunnelHealth(task)
			}
		}()
	}
	ticker := time.NewTicker(healthInterval)
	defer ticker.Stop()
	schedule := func() {
		for _, t := range m.Tunnels() {
			if pending[t] {
				continue
			}
			if task, ok := m.healthSnapshot(t); ok {
				select {
				case queue <- task:
					pending[t] = true
				default:
				}
			}
		}
	}
	for {
		select {
		case <-ticker.C:
			schedule()
		case result := <-results:
			t := result.Task.Tunnel
			delete(pending, t)
			if !m.healthResultCurrent(result) {
				delete(fails, t)
				continue
			}
			if result.Healthy {
				fails[t] = 0
				if m.latencies != nil {
					m.latencies.recordTunnelSample(result.Task.Node, result.MS, result.Target)
				}
				if result.Geo != nil && m.inspector != nil {
					m.inspector.seedGeo(*result.Geo)
				}
				m.rememberNetworkAffinity(t)
				if !t.EntryReady || t.EntryError != "" {
					m.scheduleEntryRepair(t)
				}
				continue
			}
			fails[t]++
			if fails[t] < healthFailures {
				log.Printf("隧道 %d (%s) 探测失败 %d 次", t.Slot, t.Node.HostName, fails[t])
				continue
			}

			log.Printf("隧道 %d (%s) 已掉线，正在换节点重连", t.Slot, t.Node.HostName)
			fails[t] = 0
			m.reconnectExpected(t, result.Task.Node.HostName, &result.Task)
		}
	}
}

// tunnelHealthy 判断隧道是否还真的走在 VPN 上。
//
// 只看"能不能出网"是不够的：netns 通过 veth 走母机 NAT，
// openvpn 死掉后照样能出网，只是出口变回了母机 IP。
// 所以要比对出口 IP 是否仍是建立隧道时拿到的那个。
func (m *Manager) tunnelHealthy(t *Tunnel) bool {
	task, ok := m.healthSnapshot(t)
	return ok && probeTunnelHealth(task).Healthy
}

// reconnect 就地把一条隧道换到别的节点上，保持槽位与端口不变，
// 这样已经分发出去的客户端配置仍然可用。
//
// oldHost 必须是本次重连前那条隧道真正绑着的节点名。调用方若已经
// 改过 t.Node（比如手动换节点），就要把改之前的名字传进来，
// 否则 rebind 找不到旧绑定，入站会掉成孤儿。
func (m *Manager) reconnect(t *Tunnel, oldHost string) {
	m.reconnectExpected(t, oldHost, nil)
}

func (m *Manager) reconnectExpected(t *Tunnel, oldHost string, expected *healthTask) {
	m.rememberNetworkAffinity(t)
	if !m.beginReconnect(t, expected) {
		return
	}
	if m.latencies != nil {
		m.latencies.mu.Lock()
		delete(m.latencies.tunnelSamples, latencyKey(t.Node))
		m.latencies.mu.Unlock()
	}

	// Persist the original binding and outage history before switching nodes.
	t.setPrevHost(oldHost)
	if err := m.saveState(); err != nil {
		log.Printf("保存状态失败: %v", err)
	}

	if t.ovpn != nil && t.ovpn.Process != nil {
		_ = t.ovpn.Process.Kill()
		t.ovpn = nil
	}
	t.teardownNetns()

	go func() {
		// 通知延后到 rebind/resync 之后：那两步会把入站改绑到新节点，
		// 提前重建配置会因为入站还指着旧节点名而丢掉路由规则
		m.bringUpPersist(t, false, true)
		if t.Status != "up" {
			return
		}
		// 出站 tag 跟着节点名走，换了节点就要把原来指向它的入站重新绑过去，
		// 否则面板里的路由会指向一个已经不存在的出站。
		if t.Node.HostName != oldHost {
			if err := m.rebind(oldHost, t); err != nil {
				log.Printf("重连后同步 3x-ui 绑定失败: %v", err)
				return
			}
		} else if err := m.resync(t); err != nil {
			// 节点名没变也要重写一次出站：出口 IP 可能变了，
			// 而且上一轮换节点时留下的绑定需要重新回来。
			log.Printf("重连后重写 3x-ui 出站失败: %v", err)
			return
		}
		t.setPrevHost("")
		if err := m.saveState(); err != nil {
			log.Printf("保存状态失败: %v", err)
		}
	}()
}

func (m *Manager) beginReconnect(t *Tunnel, expected *healthTask) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || t.Status == "starting" || t.Status == "stopped" {
		return false
	}
	if expected != nil {
		if expected.Tunnel != t || expected.Slot != t.Slot || t.Status != "up" || t.ExitIP != expected.ExitIP || latencyKey(t.Node) != latencyKey(expected.Node) {
			return false
		}
		t.recordAutoReconnect(time.Now())
	}
	t.Status = "starting"
	t.Err = "正在换节点重连"
	t.ExitIP = ""
	t.EntryReady = t.TemplateID == 0
	return true
}
