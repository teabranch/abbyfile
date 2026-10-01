package definition

import "fmt"

// ReturnField is one named field a sub-agent must include, in full, in its
// final message (see the return_contract: frontmatter block).
type ReturnField struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ReturnContractDef is the parsed return_contract frontmatter block.
type ReturnContractDef struct {
	Fields []ReturnField `yaml:"fields"`
}

// validateReturnContract checks field names are present, safe and unique,
// and returns the fields (nil when rc is nil).
func validateReturnContract(rc *ReturnContractDef) ([]ReturnField, error) {
	if rc == nil {
		return nil, nil
	}
	seen := make(map[string]bool, len(rc.Fields))
	for i, f := range rc.Fields {
		if f.Name == "" {
			return nil, fmt.Errorf("return_contract.fields[%d]: name is required", i)
		}
		if !validName.MatchString(f.Name) {
			return nil, fmt.Errorf("return_contract.fields[%d]: name %q contains invalid characters (only alphanumeric, hyphens, underscores allowed)", i, f.Name)
		}
		if seen[f.Name] {
			return nil, fmt.Errorf("return_contract.fields[%d]: duplicate name %q", i, f.Name)
		}
		seen[f.Name] = true
	}
	return rc.Fields, nil
}
