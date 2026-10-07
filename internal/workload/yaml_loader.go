package workload

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlWorkload mirrors the raw YAML structure for unmarshalling.
type yamlWorkload struct {
	Description string               `yaml:"description"`
	Blocks      map[string]yamlBlock `yaml:"blocks"`
	// We don't parse "scenarios" — it's nb5-specific and ignored here.
}

type yamlBlock struct {
	Ratio  interface{}            `yaml:"ratio"` // may be int or TEMPLATE string
	Params map[string]interface{} `yaml:"params"`
	Ops    interface{}            `yaml:"ops"` // list or map
}

// paramRe extracts TEMPLATE(name,default) from strings.
var paramRe = regexp.MustCompile(`TEMPLATE\(([^,)]+),([^)]+)\)`)

// LoadFile reads a YAML workload file, extracts metadata, and compiles binding
// templates for all operations using workload defaults.
// source should be "yaml-builtin" or "yaml-custom".
func LoadFile(path, source string) (WorkloadDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return WorkloadDef{}, fmt.Errorf("yaml_loader: read %q: %w", path, err)
	}
	return parseYAML(data, path, filepath.Base(path), source)
}

// LoadBytes parses a YAML workload from raw bytes. filename is used for ID
// generation and error messages. source is "yaml-builtin" or "yaml-custom".
func LoadBytes(data []byte, filename, source string) (WorkloadDef, error) {
	return parseYAML(data, "", filename, source)
}

// parseYAML parses raw YAML bytes into a WorkloadDef.
func parseYAML(data []byte, path, filename, source string) (WorkloadDef, error) {
	var raw yamlWorkload
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return WorkloadDef{}, fmt.Errorf("yaml_loader: parse %q: %w", filename, err)
	}

	id := filenameToID(filename)
	label := idToLabel(id)

	params := extractParameters(raw)
	blocks, err := parseOrderedBlocks(data)
	if err != nil {
		return WorkloadDef{}, fmt.Errorf("yaml_loader: %q: %w", filename, err)
	}
	phases := extractPhases(blocks)

	def := WorkloadDef{
		ID:          id,
		Label:       label,
		Filename:    filename,
		Path:        path,
		Source:      source,
		Description: strings.TrimSpace(raw.Description),
		Phases:      phases,
		Parameters:  params,
		Blocks:      blocks,
	}

	// Pre-compile templates using workload defaults only.
	defaults := make(map[string]string, len(params))
	for k, p := range params {
		defaults[k] = p.Default
	}
	if err := def.CompileTemplates(defaults); err != nil {
		// Non-fatal for unknown tokens at load time — they may be resolved at run time.
		// Return the def with nil templates; CompileTemplates will be called again at run time.
		_ = err
	}

	return def, nil
}

// extractParameters scans all block op CQL strings for TEMPLATE macros and
// builds the parameters map.
func extractParameters(raw yamlWorkload) map[string]Parameter {
	params := make(map[string]Parameter)
	for _, block := range raw.Blocks {
		scanForParams(fmt.Sprintf("%v", block.Ratio), params)
		for _, op := range opsFromRaw(block.Ops) {
			scanForParams(op, params)
		}
	}
	return params
}

// scanForParams finds all TEMPLATE(name,default) occurrences in s and adds them
// to the params map (without overwriting existing entries).
func scanForParams(s string, params map[string]Parameter) {
	for _, sub := range paramRe.FindAllStringSubmatch(s, -1) {
		name := strings.TrimSpace(sub[1])
		def := strings.TrimSpace(sub[2])
		if _, exists := params[name]; !exists {
			t := "string"
			// Heuristic: if the default looks like a number, mark it as "number".
			if isNumeric(def) {
				t = "number"
			}
			params[name] = Parameter{
				Default: def,
				Type:    t,
				Label:   paramNameToLabel(name),
			}
		}
	}
}

// parseOrderedBlocks parses the "blocks" key from the raw YAML preserving
// declaration order, which Go's map[string]... does not guarantee.
func parseOrderedBlocks(data []byte) ([]Block, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil
	}

	// Find the "blocks" key.
	var blocksNode *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "blocks" {
			blocksNode = root.Content[i+1]
			break
		}
	}
	if blocksNode == nil || blocksNode.Kind != yaml.MappingNode {
		return nil, nil
	}

	var blocks []Block
	for i := 0; i+1 < len(blocksNode.Content); i += 2 {
		name := blocksNode.Content[i].Value
		blockNode := blocksNode.Content[i+1]

		b := Block{Name: name, Ratio: 1, Prepared: true}

		if blockNode.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(blockNode.Content); j += 2 {
				key := blockNode.Content[j].Value
				val := blockNode.Content[j+1]
				switch key {
				case "ratio":
					if val.Kind == yaml.ScalarNode {
						// May be an integer or a TEMPLATE string — store raw; ratio resolution
						// happens at phase dispatch. Default 1 is already set.
						if val.Tag == "!!int" {
							fmt.Sscanf(val.Value, "%d", &b.Ratio)
						}
						// TEMPLATE ratio strings are ignored here; resolved at run time.
					}
				case "params":
					if val.Kind == yaml.MappingNode {
						for k := 0; k+1 < len(val.Content); k += 2 {
							if val.Content[k].Value == "prepared" {
								b.Prepared = val.Content[k+1].Value != "false"
							}
						}
					}
				case "ops":
					b.Ops = parseOps(val)
				}
			}
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// parseOps extracts ops from a yaml.Node (either a sequence or mapping).
func parseOps(node *yaml.Node) []Op {
	var ops []Op
	switch node.Kind {
	case yaml.SequenceNode:
		for idx, item := range node.Content {
			ops = append(ops, Op{
				Name:   fmt.Sprintf("op%d", idx),
				RawCQL: strings.TrimSpace(item.Value),
			})
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			ops = append(ops, Op{
				Name:   node.Content[i].Value,
				RawCQL: strings.TrimSpace(node.Content[i+1].Value),
			})
		}
	}
	return ops
}

// extractPhases deduces the canonical phase names from the block name prefixes.
func extractPhases(blocks []Block) []string {
	seen := make(map[string]bool)
	var phases []string
	for _, b := range blocks {
		phase := blockPhase(b.Name)
		if !seen[phase] {
			seen[phase] = true
			phases = append(phases, phase)
		}
	}
	return phases
}

// blockPhase maps a block name to its canonical phase name.
func blockPhase(name string) string {
	switch {
	case strings.HasPrefix(name, "schema"):
		return "schema"
	case strings.HasPrefix(name, "rampup"):
		return "rampup"
	case strings.HasPrefix(name, "main"):
		return "main"
	case strings.HasPrefix(name, "truncate"):
		return "truncate"
	default:
		return name
	}
}

// opsFromRaw extracts op CQL strings from the raw ops field (list or map).
func opsFromRaw(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	var result []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			result = append(result, fmt.Sprintf("%v", item))
		}
	case map[string]interface{}:
		for _, val := range v {
			result = append(result, fmt.Sprintf("%v", val))
		}
	}
	return result
}

// filenameToID strips the extension and converts to a slug.
func filenameToID(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	return base
}

// idToLabel converts a slug like "cql-keyvalue" to "Cql Keyvalue".
func idToLabel(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// paramNameToLabel converts "read_ratio" → "Read Ratio".
func paramNameToLabel(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
