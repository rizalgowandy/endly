package model

import (
	"fmt"
	"github.com/viant/endly/internal/util"
	"github.com/viant/endly/model/graph/yml"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"gopkg.in/yaml.v3"
	"strings"
)

// Variables a slice of variables
type Variables []*Variable

func (v Variables) MarshalYAML() (interface{}, error) {
	type variables Variables
	customVar := variables(v)
	orig := &yaml.Node{}
	err := orig.Encode(&customVar)
	if err != nil {
		return nil, err
	}
	return yml.Nodes(orig.Content).Map()
}

// Apply evaluates all variable from in map to out map
func (v *Variables) Apply(in, out data.Map) error {
	if out == nil {
		return fmt.Errorf("out state was empty")
	}
	if in == nil {
		in = data.NewMap()
	}
	for _, variable := range *v {
		if variable == nil {
			continue
		}
		if err := variable.Apply(in, out); err != nil {
			return err
		}
	}
	return nil
}

// String returns a variable info
func (v Variables) String() string {
	var result = ""
	for _, item := range v {
		if item == nil {
			continue
		}
		result += fmt.Sprintf("{Name:%v From:%v Value:%v},", item.Name, item.From, item.Value)
	}
	return result
}

func loadVariablesFromResource(baseURLs []string, resourceURI string) (Variables, error) {
	resourceURI = strings.TrimSpace(resourceURI)
	if resourceURI == "" {
		return nil, nil
	}
	var result Variables = make([]*Variable, 0)
	loaded, err := util.LoadData(baseURLs, resourceURI)
	if err == nil {
		err = toolbox.DefaultConverter.AssignConverted(&result, loaded)
	}
	return result, err
}

func isVariablesMapSource(source interface{}) bool {
	_, err := util.NormalizeMap(source, false)
	return err == nil
}

func loadVariablesFromMap(source interface{}) (Variables, error) {
	var result = make([]*Variable, 0)
	var err error
	var variable *Variable
	e := toolbox.ProcessMap(source, func(key, value interface{}) bool {
		if variable, err = newVariableFromKeyValuePair(toolbox.AsString(key), value); err != nil {
			return false
		}
		result = append(result, variable)
		return true
	})
	if e != nil {
		err = e
	}
	return result, err
}

func loadVariablesFromSlice(aSlice []interface{}) (Variables, error) {
	var result = make([]*Variable, 0)
	for _, item := range aSlice {
		switch value := item.(type) {
		case string:
			if len(value) == 0 {
				continue
			}
			expr := VariableExpression(value)
			variable, err := expr.AsVariable()
			if err != nil {
				return nil, err
			}
			result = append(result, variable)
		default:
			if toolbox.IsSlice(item) || toolbox.IsMap(item) {
				aMap, err := util.NormalizeMap(value, true)
				if err != nil {
					return nil, err
				}
				var variable = &Variable{}
				if err = toolbox.DefaultConverter.AssignConverted(&variable, aMap); err != nil {
					return nil, err
				}
				if variable.Name == "" && len(aMap) == 1 {
					for key, value := range aMap {
						variable, err = newVariableFromKeyValuePair(key, value)
						if err != nil {
							return nil, fmt.Errorf("unsupported variable definition: %v", value)
						}
					}
				}
				result = append(result, variable)
			} else {
				return nil, fmt.Errorf("unsupported type: %T", value)
			}
		}
	}
	return result, nil
}

// GetVariables returns variables from Variables ([]*Variable), []string (as expression) or from []interface{} (where interface is a map matching Variable struct)
func GetVariables(baseURLs []string, source interface{}) (Variables, error) {
	if source == nil {
		return nil, nil
	}
	switch value := source.(type) {
	case *Variables:
		return *value, nil
	case Variables:
		return value, nil
	case string:
		return loadVariablesFromResource(baseURLs, value)
	}
	var result Variables = make([]*Variable, 0)
	if !toolbox.IsSlice(source) {
		return nil, fmt.Errorf("invalid varaibles type: %T, expected %T or %T", source, result, []string{})
	}
	if isVariablesMapSource(source) {
		return loadVariablesFromMap(source)
	}
	variables := toolbox.AsSlice(source)
	if len(variables) == 0 {
		return nil, nil
	}
	return loadVariablesFromSlice(variables)
}

func newVariableFromKeyValuePair(key string, value interface{}) (*Variable, error) {
	var variable = &Variable{}
	extractFromKey(key, variable)
	textValue, isText := value.(string)
	if !isText {
		if normalized, err := toolbox.NormalizeKVPairs(value); err == nil {
			value = normalized
		}
		variable.Value = value
		return variable, nil
	}
	extractFromValue(textValue, variable)
	return variable, nil
}
