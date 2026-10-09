package netexec

import (
	"bytes"
	"encoding/json"
	"io"
)

// nft JSON is checked structurally, never by grepping for "drop". Counters and
// handles may change; every object that can change filtering semantics must match.
func rulesetObjects(raw []byte) ([]map[string]json.RawMessage, bool) {
	var doc struct {
		NFTables []map[string]json.RawMessage `json:"nftables"`
	}
	if len(raw) > 256*1024 || !unambiguousJSON(raw) || json.Unmarshal(raw, &doc) != nil || doc.NFTables == nil {
		return nil, false
	}
	for _, obj := range doc.NFTables {
		if len(obj) != 1 {
			return nil, false
		}
	}
	return doc.NFTables, true
}
func object(raw json.RawMessage, keys ...string) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k := range obj {
		if !allowed[k] {
			return nil, false
		}
	}
	return obj, true
}
func text(obj map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(obj[key], &s)
	return s
}
func emptyRuleset(raw []byte) bool {
	objects, ok := rulesetObjects(raw)
	if !ok {
		return false
	}
	for _, obj := range objects {
		if _, meta := obj["metainfo"]; !meta {
			return false
		}
	}
	return true
}
func guardMatches(raw []byte, p Plan) bool {
	objects, ok := rulesetObjects(raw)
	if !ok {
		return false
	}
	tables := 0
	chains, rules := map[string]int{}, map[string]int{}
	for _, entry := range objects {
		for kind, rawObject := range entry {
			switch kind {
			case "metainfo":
			case "table":
				o, ok := object(rawObject, "family", "name", "handle", "comment")
				if !ok || text(o, "family") != "inet" || text(o, "name") != p.Table || text(o, "comment") != p.Owner {
					return false
				}
				tables++
			case "chain":
				o, ok := object(rawObject, "family", "table", "name", "handle", "type", "hook", "prio", "policy")
				name := text(o, "name")
				var priority int
				if !ok || json.Unmarshal(o["prio"], &priority) != nil || priority != -300 || !hook(name) || text(o, "hook") != name || text(o, "family") != "inet" || text(o, "table") != p.Table || text(o, "type") != "filter" || text(o, "policy") != "drop" {
					return false
				}
				chains[name]++
			case "rule":
				o, ok := object(rawObject, "family", "table", "chain", "handle", "expr")
				name := text(o, "chain")
				if !ok || !hook(name) || text(o, "family") != "inet" || text(o, "table") != p.Table {
					return false
				}
				var expr []map[string]json.RawMessage
				if json.Unmarshal(o["expr"], &expr) != nil || len(expr) != 2 || len(expr[0]) != 1 || len(expr[1]) != 1 {
					return false
				}
				counter, exists := expr[0]["counter"]
				if !exists {
					return false
				}
				counts, ok := object(counter, "packets", "bytes")
				var packets, bytes uint64
				if !ok || json.Unmarshal(counts["packets"], &packets) != nil || json.Unmarshal(counts["bytes"], &bytes) != nil {
					return false
				}
				drop, exists := expr[1]["drop"]
				if !exists || string(drop) != "null" {
					return false
				}
				rules[name]++
			default:
				return false
			}
		}
	}
	if tables != 1 || len(chains) != 3 || len(rules) != 3 {
		return false
	}
	for _, name := range []string{"input", "forward", "output"} {
		if chains[name] != 1 || rules[name] != 1 {
			return false
		}
	}
	return true
}
func hook(s string) bool { return s == "input" || s == "forward" || s == "output" }

func onlyLoopback(raw []byte) bool {
	var links []struct {
		Name string `json:"ifname"`
		Type string `json:"link_type"`
	}
	return len(raw) <= 256*1024 && json.Unmarshal(raw, &links) == nil && len(links) == 1 && links[0].Name == "lo" && links[0].Type == "loopback"
}

// Cleanup tolerates a partially installed or drifted own table, but never
// deletes a namespace containing a foreign table, flowtable, set, or object.
func cleanupRulesetSafe(raw []byte, p Plan) bool {
	objects, ok := rulesetObjects(raw)
	if !ok {
		return false
	}
	for _, entry := range objects {
		for kind, rawObject := range entry {
			var o map[string]json.RawMessage
			if json.Unmarshal(rawObject, &o) != nil {
				return false
			}
			switch kind {
			case "metainfo":
			case "table":
				if text(o, "family") != "inet" || text(o, "name") != p.Table || text(o, "comment") != p.Owner {
					return false
				}
			case "chain", "rule":
				if text(o, "family") != "inet" || text(o, "table") != p.Table {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// Duplicate JSON keys can hide a conflicting rule or an entire ruleset. nft does
// not emit them; treat ambiguous/extended output as unverified rather than guess.
func unambiguousJSON(raw []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	count := 0
	var walk func(int) bool
	walk = func(depth int) bool {
		count++
		if depth > 32 || count > 20000 {
			return false
		}
		token, err := dec.Token()
		if err != nil {
			return false
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return true
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for dec.More() {
				token, err := dec.Token()
				if err != nil {
					return false
				}
				key, ok := token.(string)
				if !ok || keys[key] {
					return false
				}
				keys[key] = true
				if depth == 0 && key != "nftables" {
					return false
				}
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for dec.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, err := dec.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !walk(0) {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}
