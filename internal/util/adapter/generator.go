package adapter

import (
	"bytes"
	"fmt"
	"github.com/viant/endly/internal/util"
	"github.com/viant/toolbox"
	"strings"
	"text/template"
)

var adapterTemplates = map[string]string{
	"type": `//{{.TypeName}} represents request
type {{.TypeName}} struct {
{{.Fields}}
}
`,
	"field": `  {{.Name}} {{.TypeName}} {{.Tag}}`,
	"setService": `//SetService sets service
func (r * {{.TypeName}}) SetService(service interface{}) error {
	var ok bool
	if r.service_, ok = service.({{.OwnerType}}); !ok {
		return fmt.Errorf("invalid service type: %T, expected: {{.OwnerType}}", service)
	}
	return nil
}`,

	"setContext": `//SetContext sets context
func (r * {{.TypeName}}) SetContext(ctx context.Context)  {
	r.ctx = ctx
}`,

	"call": `//Call calls service
func (r * {{.TypeName}}) Call() (result interface{}, err error) {
	if r.service_ == nil {
		return nil, errors.New("service was empty")
	}
	{{.Result}}r.service_.{{.Func}}({{.Params}})
	return result, err	
}`,
	"func": `//GetId returns request id
func (r * {{.TypeName}}) GetId() string {
	return "{{.SessionID}}";	
}`,
	"register": `	register(&{{.TypeName}}{})`,

	"file": `

import (
{{.Imports}}	
)

/*autogenerated contract adapter*/

{{.Types}}

func init() {
{{.Register}}
}

{{.Methods}}
`,
}

type TypeMeta struct {
	ID              string
	OwnerPackage    string
	OwnerType       string
	SimpleOwnerType string

	TypeName     string
	SourceType   string
	Imports      string
	Methods      string
	Fields       string
	Func         string
	Params       string
	Result       string
	Embed        bool
	hasContext   bool
	importPrefix string
	aliasImports map[string]string
}

type fileMeta struct {
	Imports  string
	Types    string
	Register string
	Methods  string
}

// Generate contract for API that use interface or functions style
type Generator struct {
	templates map[string]string
}

func (g *Generator) buildTypeFields(typeInfo *toolbox.TypeInfo, typeMeta *TypeMeta, receiver *toolbox.FunctionInfo) error {
	var fields = make([]string, 0)
	serviceType := typeInfo.Package + "." + typeInfo.Name
	if typeInfo.IsStruct {
		serviceType = "*" + serviceType
	}

	serviceTypeName := typeInfo.Package + "." + typeInfo.Name
	if typeInfo.IsStruct {
		serviceTypeName = "*" + serviceTypeName
	}
	serviceField, err := g.expandTemplate("field", &toolbox.FieldInfo{
		Name:     "service_",
		TypeName: serviceTypeName,
	})

	fields = append(fields, serviceField)
	var params = make([]string, 0)
	embedded := false
	for _, param := range receiver.ParameterFields {

		if param.TypeName == "context.Context" {
			param.Name = "ctx"
			typeMeta.hasContext = true
		} else {
			param.Name = strings.Title(param.Name)
		}
		if param.TypePackage != "" {
			if param.TypePackage == typeMeta.OwnerPackage {
				param.TypeName = strings.Replace(param.TypeName, param.TypePackage, typeMeta.importPrefix, 1)
			}
			typeMeta.aliasImports[param.TypePackage] = ""
		}
		suffix := ""
		if param.IsVariant {
			suffix = "..."
		}

		embedable := typeMeta.Embed && !embedded && !isBasicType(param.TypeName)
		if embedable && param.Name != "ctx" && param.TypeName != "io.Reader" {
			param.Name = ""
			embedded = true
		}
		if serviceField, err = g.expandTemplate("field", param); err != nil {
			return err
		}
		fields = append(fields, serviceField)

		if embedable && param.Name != "ctx" && param.TypeName != "io.Reader" {
			param.Name = util.SimpleTypeName(param.TypeName)
		}

		params = append(params, "r."+param.Name+suffix)

	}
	typeMeta.Params = strings.Join(params, ",")
	typeMeta.Fields = strings.Join(fields, "\n")
	return nil
}

func (g *Generator) buildTypeMethods(typeInfo *toolbox.TypeInfo, typeMeta *TypeMeta, receiver *toolbox.FunctionInfo) error {
	switch len(receiver.ResultsFields) {
	case 1:
		if receiver.ResultsFields[0].TypeName == "error" {
			typeMeta.Result = "err = "
		} else {
			typeMeta.Result = "result = "
		}
	case 2:
		if receiver.ResultsFields[1].TypeName == "error" {
			typeMeta.Result = "result, err = "
		} else {
			typeMeta.Result = "result, _ =  "
		}
	}

	var methods = make([]string, 0)
	setServiceMethod, err := g.expandTemplate("setService", typeMeta)
	if err != nil {
		return err
	}
	methods = append(methods, setServiceMethod)

	callMethod, err := g.expandTemplate("call", typeMeta)
	if err != nil {
		return err
	}
	methods = append(methods, callMethod)

	funcMethod, err := g.expandTemplate("func", typeMeta)
	if err != nil {
		return err
	}
	methods = append(methods, funcMethod)

	typeMeta.Methods = strings.Join(methods, "\n")
	return nil
}

// GenerateMatched generated code for all matched types
func (g *Generator) GenerateMatched(source string, matcher func(typeName string) bool, predicate func(receiver *toolbox.FunctionInfo) bool, metaUpdater func(metaType *TypeMeta, receiver *toolbox.FunctionInfo)) (map[string]string, error) {
	fileset, err := toolbox.NewFileSetInfo(source)
	if err != nil {
		return nil, err
	}
	var result = make(map[string]string)
	for _, fileInfo := range fileset.FilesInfo() {
		for _, typeInfo := range fileInfo.Types() {
			if matcher(typeInfo.Name) {
				code, err := g.Generate(source, typeInfo.Name, predicate, metaUpdater)
				if err != nil {
					return nil, err
				}
				if code == nil {
					continue
				}
				result[typeInfo.Name] = *code
			}
		}
	}
	return result, nil
}

// Generate generates code
func (g *Generator) Generate(source, typeName string, predicate func(receiver *toolbox.FunctionInfo) bool, metaUpdater func(metaType *TypeMeta, receiver *toolbox.FunctionInfo)) (*string, error) {
	fileset, err := toolbox.NewFileSetInfo(source)
	if err != nil {
		return nil, err
	}
	var fileInfo *toolbox.FileInfo
	for _, candidate := range fileset.FilesInfo() {
		if candidate.HasType(typeName) {
			fileInfo = candidate
		}
	}
	if fileInfo == nil {
		return nil, nil
	}
	typeInfo := fileset.Type(typeName)
	if typeInfo == nil {
		return nil, fmt.Errorf("failed to lookup type: %v", typeName)
	}
	if len(typeInfo.Receivers()) == 0 {
		return nil, nil
	}
	var imports = make([]string, 0)

	var methods = make([]string, 0)
	imports = append(imports, `	"fmt"`, `	"errors"`)
	srcIndex := strings.Index(source, "/src/")

	if srcIndex != -1 {
		imports = append(imports, fmt.Sprintf(`	"%s"`, string(source[srcIndex+5:])))
	}
	var importMap = make(map[string]string)

	var types = make([]string, 0)
	var register = make([]string, 0)
	for _, receiver := range typeInfo.Receivers() {
		if !predicate(receiver) {
			continue
		}
		if len(receiver.ResultsFields) > 2 {
			continue
		}

		ownerType := typeInfo.Package + "." + typeInfo.Name
		if typeInfo.IsStruct {
			ownerType = "*" + ownerType
		}

		typeMeta := &TypeMeta{
			Func:            receiver.Name,
			importPrefix:    fmt.Sprintf("vvc"), //import colision prefix
			SimpleOwnerType: typeInfo.Name,
			aliasImports:    make(map[string]string),
			OwnerType:       ownerType,
			OwnerPackage:    typeInfo.Package,
			SourceType:      typeName,
		}
		metaUpdater(typeMeta, receiver)
		if typeMeta.ID == "" {
			return nil, fmt.Errorf("id was empty - revisit meta type updater logic")
		}
		if typeMeta.TypeName == "" {
			return nil, fmt.Errorf("typeName was empty - revisit meta type updater logic")
		}

		if err = g.buildTypeFields(typeInfo, typeMeta, receiver); err == nil {
			err = g.buildTypeMethods(typeInfo, typeMeta, receiver)
		}
		if typeMeta.hasContext {
			setContext, err := g.expandTemplate("setContext", typeMeta)
			if err != nil {
				return nil, err
			}
			methods = append(methods, setContext)
		}

		if err != nil {
			return nil, err
		}
		typeText, err := g.expandTemplate("type", typeMeta)
		if err != nil {
			return nil, err
		}
		registerMethod, err := g.expandTemplate("register", typeMeta)
		if err != nil {
			return nil, err
		}

		register = append(register, registerMethod)
		methods = append(methods, typeMeta.Methods)
		types = append(types, typeText)

		for k := range typeMeta.aliasImports {
			importMap[k] = receiver.Imports[k]
		}
		if path, hasCollision := importMap[typeInfo.Package]; hasCollision {
			delete(importMap, typeInfo.Package)
			importMap[typeMeta.importPrefix] = path
		}
	}

	if len(types) == 0 {
		return nil, nil
	}

	for k, v := range importMap {
		imports = append(imports, fmt.Sprintf(`	%s %s`, k, v))
	}

	fileMeta := &fileMeta{
		Imports:  strings.Join(imports, "\n"),
		Types:    strings.Join(types, "\n"),
		Register: strings.Join(register, "\n"),
		Methods:  strings.Join(methods, "\n"),
	}
	result, err := g.expandTemplate("file", fileMeta)
	return &result, nil
}

func (g *Generator) expandTemplate(templateId string, data interface{}) (string, error) {
	textTemplate, ok := g.templates[templateId]
	if !ok {
		return "", fmt.Errorf("failed to lookup template: %v", templateId)
	}
	tmpl, err := template.New(templateId).Parse(textTemplate)
	if err != nil {
		return "", fmt.Errorf("fiailed to parse template %v, due to %v", textTemplate, err)
	}
	writer := new(bytes.Buffer)
	err = tmpl.Execute(writer, data)
	return writer.String(), err
}

func New() *Generator {
	return &Generator{
		templates: adapterTemplates,
	}
}
