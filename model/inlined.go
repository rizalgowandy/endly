package model

import (
	"github.com/viant/endly/internal/util"
	"github.com/viant/endly/model/location"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"strings"
)

const (
	//CatchTask  represent a task name that execute if error occurred and defined
	CatchTask = "catch"
	//DeferredTask represent a task name that always execute if defined
	DeferredTask = "defer"
	//ExplicitActionAttributePrefix represent model attribute prefix
	ExplicitActionAttributePrefix  = ":"
	ExplicitRequestAttributePrefix = "@"

	requestKey     = "request"
	failKey        = "fail"
	parentKey      = "parent"
	actionKey      = "action"
	whenKey        = "when"
	serviceKey     = "service"
	workflowKey    = "workflow"
	skipKey        = "skip"
	loggingKey     = "logging"
	descriptionKey = "description"
	commentsKey    = "comments"
	initKey        = "init"
	postKey        = "post"
	exitKey        = "exit"
	tagKey         = "tag"
	defaultPath    = "default"
)

var multiActionKeys = []string{"multiaction", "async"}

type MapEntry struct {
	Key   string      `description:"preserved order map entry key"`
	Value interface{} `description:"preserved order map entry value"`
}

// Inlined represents inline workflow
type Inlined struct {
	baseURL    string
	tagPathURL string
	name       string
	Init       interface{}
	Post       interface{}
	Logging    *bool
	Defaults   map[string]interface{}
	Data       map[string]interface{}
	Pipeline   []*MapEntry
	State      data.Map
	workflow   *Workflow //inline workflow from pipeline
}

func (p Inlined) updatereservedAttributes(aMap map[string]interface{}) {
	for _, key := range []string{actionKey, workflowKey, skipKey, whenKey, postKey, initKey, commentsKey, descriptionKey, failKey} {
		if val, ok := aMap[key]; ok {
			if _, has := aMap[ExplicitActionAttributePrefix+key]; has {
				continue
			}
			delete(aMap, key)
			aMap[ExplicitActionAttributePrefix+key] = val
		}
	}
	for _, key := range []string{tagKey} {
		if val, ok := aMap[key]; ok {
			if _, has := aMap[ExplicitRequestAttributePrefix+key]; has {
				continue
			}
			delete(aMap, key)
			aMap[ExplicitRequestAttributePrefix+key] = val
		}
	}
}

var normalizationBlacklist = map[string]bool{
	"workflow:run":     true,
	"seleniun:run":     true,
	"validator:assert": true,
}

func isNormalizableRequest(actionAttributes map[string]interface{}) bool {
	if len(actionAttributes) == 0 {
		return true
	}
	if _, ok := actionAttributes[workflowKey]; ok {
		return false
	}

	action := ""
	if val, ok := actionAttributes[actionKey]; ok {
		action = toolbox.AsString(val)
		action = strings.Replace(action, ".", ":", 1)
	}
	if strings.Count(action, ":") == 0 {
		service := workflowKey
		if val, ok := actionAttributes[serviceKey]; ok {
			service = toolbox.AsString(val)
		}
		action = service + ":" + action
	}
	_, has := normalizationBlacklist[action]
	return !has
}

func (p Inlined) loadRequest(actionAttributes, actionRequest map[string]interface{}, state data.Map) error {
	requestMap := actionRequest
	dataRequest := data.NewMap()
	var err error

	normalizable := isNormalizableRequest(actionAttributes)

	if req, ok := actionAttributes[requestKey]; ok {
		request := toolbox.AsString(actionAttributes[requestKey])
		if strings.HasPrefix(request, "@") {
			requestMap, err = util.LoadMap([]string{p.tagPathURL, toolbox.URLPathJoin(p.baseURL, defaultPath), p.baseURL}, request)
			if err == nil {
				delete(actionAttributes, requestKey)
				delete(actionRequest, requestKey)
			}
			if state != nil && normalizable {
				requestMap = toolbox.AsMap(state.Expand(requestMap))
			} else {
				parentState := data.NewMap()
				parentState.Put(parentKey, state)
				requestMap = toolbox.AsMap(parentState.Expand(requestMap))
			}
		} else {
			requestMap, err = util.NormalizeMap(req, true)
		}
		delete(actionAttributes, requestKey)
		if err != nil {
			return err
		}
		util.Append(dataRequest, actionAttributes, true)
	}

	if len(dataRequest) > 0 {
		requestMap = toolbox.AsMap(dataRequest.Expand(requestMap))
	}

	if normalizable {
		requestMap, err = util.NormalizeMap(requestMap, true)
		if err != nil {
			return err
		}
	}
	if val, ok := requestMap["defaults"]; ok {
		if defaults, err := util.NormalizeMap(val, false); err == nil {
			requestMap["defaults"] = defaults
		}
	}
	for _, key := range []string{whenKey, initKey, postKey, skipKey, exitKey, failKey} {
		if node, ok := actionAttributes[key]; ok {
			node = dataRequest.Expand(node)
			actionAttributes[key] = state.Expand(node)
		}
	}
	util.Append(actionRequest, requestMap, true)

	return nil
}

func (p Inlined) asVariables(source interface{}) ([]map[string]interface{}, error) {
	if source == nil {
		return nil, nil
	}
	var result = make([]map[string]interface{}, 0)
	variables, err := GetVariables([]string{p.tagPathURL, p.baseURL}, source)
	if err != nil {
		return nil, err
	}
	err = toolbox.DefaultConverter.AssignConverted(&result, variables)
	return result, err
}

// groupAttributes splits key value pair into workflow action attribute and action request data,
// while ':' key prefix assign pair to workflow action, '@' assign to request data, if none is matched pair is assign to both
func (p Inlined) groupAttributes(source interface{}, state data.Map) (map[string]interface{}, map[string]interface{}, error) {
	aMap, err := util.NormalizeMap(source, false)
	var actionAttributes = make(map[string]interface{})
	var actionRequest = make(map[string]interface{})
	p.updatereservedAttributes(aMap)

	for k, v := range aMap {
		if strings.HasPrefix(k, ExplicitActionAttributePrefix) {
			actionAttributes[strings.ToLower(string(k[1:]))] = v
			continue
		}
		if strings.HasPrefix(k, ExplicitRequestAttributePrefix) {
			actionRequest[strings.ToLower(string(k[1:]))] = v
			continue
		}
		actionAttributes[k] = v
		actionRequest[k] = v
	}

	if err = p.loadRequest(actionAttributes, actionRequest, state); err != nil {
		return nil, nil, err
	}
	if value, ok := actionAttributes[loggingKey]; ok {
		actionAttributes[loggingKey] = toolbox.AsBoolean(value)
	}
	err = p.loadVariables(actionAttributes, state)
	return actionAttributes, actionRequest, err
}

func (p *Inlined) loadVariables(actionAttributes map[string]interface{}, state data.Map) error {
	for _, key := range []string{initKey, postKey} {
		value, ok := actionAttributes[key]
		if !ok {
			continue
		}
		variables, err := p.asVariables(value)
		if err != nil {
			delete(actionAttributes, key)
			return err
		} else {
			actionAttributes[key] = state.Expand(variables)
		}
	}
	return nil
}

func (p *Inlined) AsWorkflow(name string, baseURL string) (*Workflow, error) {
	if p.workflow != nil {
		return p.workflow, nil
	}
	p.baseURL = baseURL
	p.name = name
	if len(p.Data) == 0 {
		p.Data = make(map[string]interface{})
	}
	var workflow = &Workflow{
		AbstractNode: &AbstractNode{
			Name:    name,
			Logging: p.Logging,
		},
		TasksNode: &TasksNode{
			Tasks: []*Task{},
		},
		Data:   p.Data,
		Source: location.NewResource(toolbox.URLPathJoin(baseURL, name+".yaml")),
	}
	var err error
	if p.Init != nil {
		if workflow.AbstractNode.Init, err = GetVariables([]string{p.baseURL}, p.Init); err != nil {
			return nil, err
		}
	}
	if p.Post != nil {
		if workflow.AbstractNode.Post, err = GetVariables([]string{p.baseURL}, p.Post); err != nil {
			return nil, err
		}
	}
	root := p.buildTask("", map[string]interface{}{})
	tagID := name

	if len(p.Pipeline) > 0 {
		for _, entry := range p.Pipeline {
			if err = p.buildWorkflowNodes(entry.Key, entry.Value, root, tagID, p.State); err != nil {
				return nil, err
			}
		}
	}

	if len(root.Tasks) > 0 {
		p.normalize(root.TasksNode)
		workflow.TasksNode = root.TasksNode
	} else {
		workflow.TasksNode = &TasksNode{
			Tasks: []*Task{root},
		}
	}
	p.workflow = workflow
	return workflow, nil
}

func (p *Inlined) normalize(node *TasksNode) {
	for _, task := range node.Tasks {
		if task.Name == CatchTask {
			node.OnErrorTask = task.Name
		}
		if task.Name == DeferredTask {
			node.DeferredTask = task.Name
		}
		p.normalize(task.TasksNode)
	}
}

func (p *Inlined) buildTask(name string, source interface{}) *Task {
	var task = &Task{}
	if toolbox.IsSlice(source) && toolbox.IsMap(source) {
		_ = toolbox.DefaultConverter.AssignConverted(task, source)
	}
	task.Actions = []*Action{}
	task.AbstractNode = &AbstractNode{}
	task.TasksNode = &TasksNode{
		Tasks: []*Task{},
	}
	task.Name = name
	return task
}

func isActionNode(attributes map[string]interface{}) bool {
	if len(attributes) == 0 {
		return false
	}
	_, action := attributes[actionKey]
	_, workflow := attributes[workflowKey]
	return action || workflow
}

func getTemplateNode(source interface{}) *TransientTemplate {
	if source == nil || !(toolbox.IsSlice(source) || toolbox.IsMap(source)) {
		return nil
	}
	var template = &TransientTemplate{}
	err := toolbox.DefaultConverter.AssignConverted(template, source)
	if err != nil || len(template.Template) == 0 || template.SubPath == "" {
		return nil
	}
	return template
}

func (p *Inlined) buildAction(name string, actionAttributes, actionRequest map[string]interface{}, tagId string) (*Action, error) {
	var result = &Action{
		AbstractNode:   &AbstractNode{},
		ServiceRequest: &ServiceRequest{},
		Repeater:       &Repeater{},
	}

	util.Append(actionRequest, p.Defaults, false)

	if action, ok := actionAttributes[actionKey]; ok {
		actionAttributes[requestKey], _ = util.NormalizeMap(actionRequest, false)
		selector := ActionSelector(toolbox.AsString(action))
		actionAttributes[serviceKey] = selector.Service()
		actionAttributes[actionKey] = selector.Action()
	} else {
		workflow := toolbox.AsString(actionAttributes[workflowKey])
		actionAttributes[actionKey] = "run"
		selector := WorkflowSelector(workflow)
		actionAttributes[requestKey] = map[string]interface{}{
			"params": actionRequest,
			"tasks":  selector.Tasks(),
			"URL":    selector.URL(),
		}
	}
	if err := toolbox.DefaultConverter.AssignConverted(result, actionAttributes); err != nil {
		return nil, err
	}
	_ = result.Init()
	if result.Name == "" {
		result.Name = name
	}
	if result.Tag == "" {
		result.Tag = name
	}
	if result.TagID == "" {
		result.TagID = tagId
	}
	if result.TagID == "" {
		result.TagID = name
	}
	return result, nil
}

func (p *Inlined) hasActionNode(source interface{}) bool {
	if source == nil {
		return false
	}
	var result = false
	attributes, _ := util.NormalizeMap(source, false)

	if isActionNode(attributes) {
		return true
	}

	_ = toolbox.ProcessMap(attributes, func(key, value interface{}) bool {
		if value == nil {
			return true
		}
		if !(toolbox.IsMap(value) || toolbox.IsStruct(value) || toolbox.IsSlice(value)) {
			return true
		}
		if p.hasActionNode(value) {
			result = true
			return false
		}
		return true
	})
	return result
}

func (p *Inlined) buildWorkflowNodes(name string, source interface{}, parentTask *Task, tagID string, state data.Map) error {
	if state != nil {
		source = state.Expand(source)
	}
	actionAttributes, actionRequest, err := p.groupAttributes(source, state)
	if err != nil {
		return err
	}
	var task *Task
	isTemplateNode := false
	if parentTask != nil {
		template := getTemplateNode(source)
		if template != nil {
			task = p.buildTask(name, source)
			parentTask.Tasks = append(parentTask.Tasks, task)
			isTemplateNode = true
			if err = template.Expand(task, name, p); err != nil {
				return err
			}
		}
	}

	if isActionNode(actionAttributes) {
		if isNormalizableRequest(actionAttributes) {
			if normalized, err := util.NormalizeMap(actionRequest, true); err == nil {
				actionRequest = normalized
			}
		}
		action, err := p.buildAction(name, actionAttributes, actionRequest, tagID)
		if err != nil {
			return err
		}
		task := parentTask
		if !parentTask.multiAction {
			task = p.buildTask(name, map[string]interface{}{})
			parentTask.Tasks = append(parentTask.Tasks, task)
		}

		if action.Description != "" && task.Description == "" {
			task.Description = action.Description
		}
		task.Actions = append(task.Actions, action)

		if reset, ok := actionAttributes[failKey]; ok {
			task.Fail = toolbox.AsBoolean(reset)
		}
		return nil
	}

	if !p.hasActionNode(actionAttributes) {
		return nil
	}

	if !isTemplateNode {
		task = p.buildTask(name, source)
		parentTask.Tasks = append(parentTask.Tasks, task)
	}

	var nodeAttributes = make(map[string]interface{})
	var buildErr error

	if err := toolbox.ProcessMap(source, func(key, value interface{}) bool {
		textKey := strings.ToLower(toolbox.AsString(key))
		if isTemplateNode && "template" == textKey {
			return true
		}
		if textKey == loggingKey || textKey == whenKey || textKey == descriptionKey || textKey == failKey { //abstract node attributes
			nodeAttributes[textKey] = value
		}
		flagAsMultiActionIfMatched(textKey, task, value)
		if value == nil || !toolbox.IsSlice(value) {
			return true
		}
		buildErr = p.buildWorkflowNodes(toolbox.AsString(key), value, task, tagID+"_"+task.Name, state)
		if buildErr != nil {
			return false
		}
		nodeAttributes[textKey] = value
		return true
	}); err != nil {
		return err
	}

	if task == nil {
		task = parentTask
	}
	if _, actionNode := nodeAttributes[actionKey]; !actionNode && !isTemplateNode {
		if taskAttributes, _, err := p.groupAttributes(nodeAttributes, state); err == nil {
			if len(taskAttributes) > 0 {
				tempTask := &Task{TasksNode: &TasksNode{}}
				if err = toolbox.DefaultConverter.AssignConverted(&tempTask, taskAttributes); err == nil {
					if tempTask.AbstractNode != nil {
						task.Init = tempTask.Init
						task.Post = tempTask.Post
						task.When = tempTask.When
						task.Logging = tempTask.Logging
						task.Description = tempTask.Description
					}
				}
			}
		}
	}
	return buildErr
}

func flagAsMultiActionIfMatched(textKey string, task *Task, value interface{}) {
	for _, key := range multiActionKeys {
		if textKey == key && toolbox.IsBool(value) {
			task.multiAction = toolbox.AsBoolean(value)
			break
		}
	}
}
