package domain

import (
	"errors"
	"strings"
)

type RecoveryPolicy struct {
	Enabled       bool     `json:"enabled"`
	ResumeIngress bool     `json:"resume_ingress"`
	AllowLegacy   bool     `json:"allow_legacy"`
	NodeIDs       []string `json:"node_ids"`
	Confirm       bool     `json:"confirm,omitempty"`
}

func (p RecoveryPolicy) Validate() error {
	if !p.Confirm {
		return errors.New("自动恢复/候选换线会在故障和控制重启后联网；需明确确认")
	}
	if len(p.NodeIDs) > 3 {
		return errors.New("自动候选最多3个，不批量压测志愿节点")
	}
	seen := map[string]bool{}
	for _, id := range p.NodeIDs {
		if len(id) < 1 || len(id) > 128 || strings.ContainsAny(id, "\x00\r\n /\\") || seen[id] {
			return errors.New("候选身份无效或重复")
		}
		seen[id] = true
	}
	return nil
}
