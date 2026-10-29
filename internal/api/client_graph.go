package api

import "fmt"

func validateClientGraph(format string, config map[string]any) error {
	if format != "sing-box" {
		return nil
	}
	tags := map[string]bool{}
	for _, v := range array(config["outbounds"]) {
		tag := text(object(v)["tag"])
		if tag == "" || tags[tag] {
			return fmt.Errorf("出站名称为空或重复: %s", tag)
		}
		tags[tag] = true
	}
	check := func(value any) error {
		if ref, ok := value.(string); ok && ref != "" && !tags[ref] {
			return fmt.Errorf("客户端配置引用不存在的出站: %s", ref)
		}
		return nil
	}
	for _, v := range array(config["outbounds"]) {
		m := object(v)
		if err := check(m["detour"]); err != nil {
			return err
		}
		for _, ref := range stringList(m["outbounds"]) {
			if err := check(ref); err != nil {
				return err
			}
		}
	}
	route := object(config["route"])
	if err := check(route["final"]); err != nil {
		return err
	}
	for _, v := range array(route["rules"]) {
		if err := check(object(v)["outbound"]); err != nil {
			return err
		}
	}
	for _, v := range array(route["rule_set"]) {
		if err := check(object(v)["download_detour"]); err != nil {
			return err
		}
	}
	for _, v := range array(object(config["dns"])["servers"]) {
		if err := check(object(v)["detour"]); err != nil {
			return err
		}
	}
	return nil
}
func stringList(v any) []string {
	if a, ok := v.([]string); ok {
		return a
	}
	a := []string{}
	for _, v := range array(v) {
		if s, ok := v.(string); ok {
			a = append(a, s)
		}
	}
	return a
}
func array(v any) []any { a, _ := v.([]any); return a }
