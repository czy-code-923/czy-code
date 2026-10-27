package api

import (
	"gopkg.in/yaml.v3"
	"vps-control/internal/clientpolicy"
)

func marshalPolicyYAML(p clientpolicy.Policy) (string, error) {
	b, e := yaml.Marshal(p)
	return string(b), e
}
