package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"mikugate/internal/runlock"
	"mikugate/internal/store"
	"os"
	"path/filepath"
	"runtime"
)

type node struct {
	LegacyExitID string                  `json:"legacy_exit_id,omitempty"`
	Name         string                  `json:"name"`
	Material     ingress.ManagedMaterial `json:"material"`
	Routes       []ingress.LandingRoute  `json:"routes"`
}
type landing struct {
	Exit     domain.Exit `json:"exit"`
	Node     domain.Node `json:"node"`
	Config   string      `json:"config"`
	Password string      `json:"password"`
}
type export struct {
	Instance string    `json:"instance"`
	Nodes    []node    `json:"nodes"`
	Landings []landing `json:"landings"`
	Skipped  int       `json:"skipped"`
}

func run() error {
	data := flag.String("data", "", "原安装数据目录")
	instance := flag.String("expected-instance", "", "原实例ID")
	flag.Parse()
	if flag.NArg() != 0 || !filepath.IsAbs(*data) || *instance == "" || runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return fmt.Errorf("兼容导出需要原普通账号、data和expected-instance")
	}
	actual, err := store.InspectInstance(filepath.Join(*data, "miku-gate.db"))
	if err != nil || actual != *instance {
		return fmt.Errorf("原实例不匹配")
	}
	lock, err := runlock.Acquire(filepath.Join(*data, ".serve.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	s, err := store.Open(filepath.Join(*data, "miku-gate.db"))
	if err != nil {
		return err
	}
	defer s.Close()
	result := export{Instance: actual, Nodes: []node{}, Landings: []landing{}}
	exits, err := s.ListExits()
	if err != nil {
		return err
	}
	ids, err := s.EntranceIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		view, base, routes, err := s.EntranceRoutes(id)
		if err != nil {
			return err
		}
		if base.External == nil {
			result.Nodes = append(result.Nodes, node{Name: view.Name, Material: base, Routes: routes})
		}
	}
	for _, exit := range exits {
		material, _, configured, err := s.Managed(exit.ID)
		if err != nil {
			return err
		}
		if configured {
			result.Nodes = append(result.Nodes, node{Name: exit.Name, LegacyExitID: exit.ID, Material: material, Routes: []ingress.LandingRoute{}})
		}
		vpn, configured, err := s.VPN(exit.ID)
		if err != nil {
			return err
		}
		if !configured {
			result.Skipped++
			continue
		}
		if vpn.FrontProxy != nil {
			result.Skipped++
			continue
		}
		snapshot, err := s.NodeForRuntime(vpn.NodeID)
		if err != nil {
			return err
		}
		password, err := s.ProxyPassword(exit.ID)
		if err != nil {
			return err
		}
		result.Landings = append(result.Landings, landing{Exit: exit, Node: snapshot, Config: snapshot.Config, Password: password})
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "原数据兼容导出失败，配套备份保留")
		os.Exit(1)
	}
}
