package workflow

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/viant/afs"
	"github.com/viant/afs/url"
	"github.com/viant/endly"
	"github.com/viant/endly/model"
	"github.com/viant/endly/model/criteria"
	"github.com/viant/endly/model/location"
	"github.com/viant/endly/model/msg"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"log"
	"os"
	"path"
	"strings"
	"sync"
)

const (
	//ServiceID represents workflow Service id
	ServiceID = "workflow"
)

// Service represents a workflow service.
type Service struct {
	*endly.AbstractService
	registry  map[string]*model.Workflow
	converter *toolbox.Converter
	fs        afs.Service
}

func (s *Service) registerWorkflow(request *RegisterRequest) (*RegisterResponse, error) {
	if err := s.Register(request.Workflow); err != nil {
		return nil, err
	}
	var response = &RegisterResponse{
		Source: request.Workflow.Source,
	}
	return response, nil
}

// Register register workflow.
func (s *Service) Register(workflow *model.Workflow) error {
	err := workflow.Validate()
	if err != nil {
		return err
	}
	s.registry[workflow.Name] = workflow
	return nil
}

// HasWorkflow returns true if service has registered workflow.
func (s *Service) HasWorkflow(name string) bool {
	_, found := s.registry[name]
	return found
}

// Workflow returns a workflow for supplied name.
func (s *Service) Workflow(name string) (*model.Workflow, error) {
	s.Lock()
	defer s.Unlock()
	if result, found := s.registry[name]; found {
		return result, nil
	}
	return nil, fmt.Errorf("failed to lookup workflow: %v", name)
}

func (s *Service) addVariableEvent(name string, variables model.Variables, context *endly.Context, in, out data.Map) {
	if len(variables) == 0 {
		return
	}
	context.Publish(model.NewModifiedStateEvent(variables, in, out))
}

func (s *Service) runAction(context *endly.Context, action *model.Action, process *model.Process) (response map[string]interface{}, err error) {
	var state = context.State()

	var activity *model.Activity
	_ = runWithoutSelfIfNeeded(process, action, state, func() error {
		activity = model.NewActivity(context, action, state)
		return nil
	})
	s.Mutex().Lock()
	process.State.Put("index", action.TagIndex)
	s.Mutex().Unlock()

	defer func() {
		var resultKey = action.Name
		if resultKey == "" {
			resultKey = action.Action
		}
		if err != nil {
			err = fmt.Errorf("%v: %v", action.TagID, err)
		} else if len(response) > 0 {
			state.Put(resultKey, response)
			var variables = model.Variables{
				{
					Name:  resultKey,
					Value: response,
				},
			}
			_ = variables.Apply(state, state)
			context.Publish(model.NewModifiedStateEvent(variables, state, state))
		}
	}()
	var request interface{}
	err = s.runNode(context, "action", process, action.AbstractNode, func(context *endly.Context, process *model.Process) (in, out data.Map, err error) {
		process.Push(activity)
		startEvent := s.Begin(context, activity)
		defer s.End(context)(startEvent, model.NewActivityEndEvent(activity))
		defer process.Pop()

		requestMap := toolbox.AsMap(activity.Request)
		if err = runWithoutSelfIfNeeded(process, action, state, func() error {
			request, err = context.AsRequest(activity.Service, activity.Action, requestMap)
			return err
		}); err != nil {
			return nil, nil, err
		}
		err = endly.Run(context, request, activity.ServiceResponse)
		if err != nil {
			return nil, nil, err
		}

		_ = toolbox.DefaultConverter.AssignConverted(&activity.Response, activity.ServiceResponse.Response)
		response = activity.Response
		if runResponse, ok := activity.ServiceResponse.Response.(*RunResponse); ok {
			response = runResponse.Data
		}
		return response, state, err
	})
	return response, err
}

func (s *Service) runTask(context *endly.Context, process *model.Process, task *model.Task) (data.Map, error) {
	process.SetTask(task)
	var result = data.NewMap()
	var state = context.State()

	// Determine owner URL for task context
	var ownerURL string
	if process != nil && process.Source != nil {
		ownerURL = process.Source.URL
	} else if process != nil && process.Workflow != nil && process.Workflow.Source != nil {
		ownerURL = process.Workflow.Source.URL
	}
	// Prefer task.MetaTag, fallback to first non-async action MetaTag
	var templateTag *model.MetaTag
	if task.MetaTag != nil {
		tagCopy := *task.MetaTag
		templateTag = &tagCopy
	} else {
		for _, a := range task.Actions {
			if a != nil && !a.Async && a.MetaTag != nil {
				tagCopy := *a.MetaTag
				templateTag = &tagCopy
				break
			}
		}
	}
	// Minimal task path (logger will compose full hierarchy from start/end stacks)
	taskPath := []string{task.Name}
	// Publish TaskStartEvent
	_ = context.Publish(model.NewTaskStartEvent(
		process.Workflow.Name,
		ownerURL,
		task.Name,
		taskPath,
		context.SessionID,
		0,
		templateTag,
	))

	asyncGroup := &sync.WaitGroup{}
	var asyncError error
	asyncActions := task.AsyncActions()

	err := s.runNode(context, "task", process, task.AbstractNode, func(context *endly.Context, process *model.Process) (in, out data.Map, err error) {
		if task.TasksNode != nil && len(task.Tasks) > 0 {
			if err := s.runTasks(context, process, task.TasksNode); err != nil || len(task.Actions) == 0 {
				return state, result, err
			}
		}
		if len(asyncActions) > 0 {
			// Publish async start
			_ = context.Publish(model.NewTaskAsyncStartEvent(taskPath, len(asyncActions), context.SessionID))
			s.runAsyncActions(context, process, task, asyncActions, asyncGroup, &asyncError)
		}
		for i := 0; i < len(task.Actions); i++ {
			action := task.Actions[i]
			if action.Async {
				continue
			}
			if process.HasTagID && !process.TagIDs[action.TagID] {
				continue
			}
			var handler = func(action *model.Action) func() (interface{}, error) {
				return func() (interface{}, error) {
					var response, err = s.runAction(context, action, process)
					if err != nil {
						return nil, err
					}
					if len(response) > 0 {
						result[action.ID()] = response
					}
					return response, nil
				}
			}
			moveToNextTag, err := criteria.Evaluate(context, context.State(), action.Skip, action.SkipEval(), "Skip", false)
			if err != nil {
				return nil, nil, err
			}
			if moveToNextTag {
				for j := i + 1; j < len(task.Actions) && action.TagID == task.Actions[j].TagID; j++ {
					i++
				}
				continue
			}
			var extractable = make(map[string]interface{})
			err = action.Repeater.Run(context, "action", s.AbstractService, handler(task.Actions[i]), extractable)
			if err != nil {
				return nil, nil, err
			}
		}

		return state, result, nil
	})

	if len(asyncActions) > 0 {
		_ = s.RunInBackground(context, func() error {
			context.Publish(msg.NewStdoutEvent("async", "waiting for actions ..."))
			asyncGroup.Wait()
			// Publish async done
			_ = context.Publish(model.NewTaskAsyncDoneEvent(taskPath, len(asyncActions), context.SessionID))
			return nil
		})
		if err == nil && asyncError != nil {
			err = asyncError
		}
	}
	state.Apply(result)
	// Publish TaskEndEvent
	status := "ok"
	errMsg := ""
	if err != nil {
		status = "error"
		errMsg = fmt.Sprintf("%v", err)
	}
	_ = context.Publish(model.NewTaskEndEvent(
		process.Workflow.Name,
		ownerURL,
		task.Name,
		taskPath,
		context.SessionID,
		0,
		status,
		errMsg,
	))
	return result, err
}

func (s *Service) runAsyncAction(parent, context *endly.Context, process *model.Process, action *model.Action, group *sync.WaitGroup) error {
	defer group.Done()
	events := context.MakeAsyncSafe()
	defer func() {
		for _, event := range events.Events {
			parent.Publish(event)
		}
	}()
	var result = make(map[string]interface{})
	var handler = func(action *model.Action) func() (interface{}, error) {
		return func() (interface{}, error) {
			var response, err = s.runAction(context, action, process)
			if err != nil {
				return nil, err
			}
			if len(response) > 0 {
				result[action.ID()] = response
			}
			return response, nil
		}
	}

	var extractable = make(map[string]interface{})
	err := action.Repeater.Run(context, "action", s.AbstractService, handler(action), extractable)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) runAsyncActions(context *endly.Context, process *model.Process, task *model.Task, asyncAction []*model.Action, group *sync.WaitGroup, asyncError *error) {
	if len(asyncAction) > 0 {
		group.Add(len(asyncAction))
		var groupErr error
		for i := range asyncAction {
			context.Publish(NewAsyncEvent(asyncAction[i]))
			go func(action *model.Action, actionContext *endly.Context) {
				if err := s.runAsyncAction(context, actionContext, process, action, group); err != nil {
					groupErr = err
				}
			}(asyncAction[i], context.Clone())
		}
		if groupErr != nil {
			*asyncError = groupErr
		}
	}
}

func (s *Service) applyVariables(candidates interface{}, process *model.Process, in data.Map, context *endly.Context) error {
	variables, ok := candidates.(model.Variables)
	if !ok || len(variables) == 0 {
		return nil
	}

	var out = context.State()
	err := variables.Apply(in, out)
	s.addVariableEvent("Pipeline", variables, context, in, out)
	return err
}

func (s *Service) run(context *endly.Context, request *RunRequest) (response *RunResponse, err error) {
	if request.Async {
		context.Wait.Add(1)
		go func() {
			defer context.Publish(NewEndEvent(context.SessionID))
			defer context.Wait.Done()
			_, err = s.runWorkflow(context, request)
			if err != nil {
				context.Publish(msg.NewErrorEvent(fmt.Sprintf("%v", err)))
			}
		}()
		return &RunResponse{}, nil
	}
	defer context.Publish(NewEndEvent(context.SessionID))
	return s.runWorkflow(context, request)
}

func (s *Service) enableLoggingIfNeeded(context *endly.Context, request *RunRequest) {
	if request.EnableLogging && !context.HasLogger {
		subdir := context.SessionID
		if request.LogSubdir != "" {
			subdir = request.LogSubdir
		}
		var logDirectory = path.Join(request.LogDirectory, subdir)
		logger := NewLogger(logDirectory, context.Listener)
		context.Listener = logger.AsEventListener()
	}
}

// NewRepoResource returns new woorkflow repo resource, it takes context map and resource URI
func (d *Service) NewRepoResource(ctx context.Context, state data.Map, URI string) (*location.Resource, error) {
	URI = state.ExpandAsText(URI)
	ok, err := d.fs.Exists(ctx, URI)
	if ok {
		return location.NewResource(URI), nil
	}
	URL := url.Join("mem://github.com/viant/endly", URI)
	if ok, _ = d.fs.Exists(ctx, URL); ok {
		return location.NewResource(URL), nil
	}
	return location.NewResource(URI), err
}

func (s *Service) publishParameters(request *RunRequest, context *endly.Context) map[string]interface{} {
	var state = context.State()
	params := buildParamsMap(request, context)
	if request.PublishParameters {
		for key, value := range params {
			state.Put(key, value)
		}
	}
	state.Put(paramsStateKey, params)
	return params
}

func (s *Service) getWorkflow(context *endly.Context, request *RunRequest) (*model.Workflow, error) {
	if request.workflow != nil {
		context.Publish(NewLoadedEvent(request.workflow))
		return request.workflow, nil
	}
	workflow, err := s.Workflow(request.Name)
	if err != nil {
		return nil, err
	}
	context.Publish(NewLoadedEvent(workflow))
	return workflow, err
}

func (s *Service) runWorkflow(upstreamContext *endly.Context, request *RunRequest) (response *RunResponse, err error) {
	response = &RunResponse{
		Data:      make(map[string]interface{}),
		SessionID: upstreamContext.SessionID,
	}

	s.enableLoggingIfNeeded(upstreamContext, request)
	workflow, err := s.getWorkflow(upstreamContext, request)
	if err != nil {
		return nil, err
	}

	// Emit WorkflowStartEvent with parent linkage and prepare paired end event
	var parentName, parentOwnerURL string
	if parent := LastWorkflow(upstreamContext); parent != nil && parent.Workflow != nil {
		parentName = parent.Workflow.Name
		if parent.Source != nil {
			parentOwnerURL = parent.Source.URL
		} else if parent.Workflow.Source != nil {
			parentOwnerURL = parent.Workflow.Source.URL
		}
	}
	var ownerURL string
	if workflow != nil && workflow.Source != nil {
		ownerURL = workflow.Source.URL
	}
	startWorkflowEvent := upstreamContext.Publish(NewWorkflowStartEvent(
		workflow.Name,
		ownerURL,
		parentName,
		parentOwnerURL,
		upstreamContext.SessionID,
		request.Tasks,
		request.TagIDs,
	))
	defer func() {
		status := "ok"
		errMsg := ""
		if err != nil {
			status = "error"
			errMsg = fmt.Sprintf("%v", err)
		}
		_ = upstreamContext.PublishWithStartEvent(NewWorkflowEndEvent(
			workflow.Name,
			ownerURL,
			parentName,
			parentOwnerURL,
			upstreamContext.SessionID,
			status,
			errMsg,
		), startWorkflowEvent)
	}()

	defer Pop(upstreamContext)

	upstreamProcess := Last(upstreamContext)
	process := model.NewProcess(workflow.Source, workflow, upstreamProcess)
	process.AddTagIDs(strings.Split(request.TagIDs, ",")...)
	Push(upstreamContext, process)

	process.State = data.NewMap()
	upstreamState := upstreamContext.State()
	if request.StateKey != "" {
		if upstreamState.Has(request.StateKey) {
			log.Printf("detected workflow state key: %v is taken by: %v, skiping consider stateKey customiztion\n", request.StateKey, upstreamState.Get(request.StateKey))
		}
		upstreamState.Put(request.StateKey, process.State)
		defer func() {
			upstreamState.Delete(request.StateKey)

		}()
	}

	context := upstreamContext
	state := context.State()
	if !request.SharedState {
		context = upstreamContext.Clone()
		state = context.State()
		state.Delete(selfStateKey)
	}

	origSelfState := upstreamState.Get(selfStateKey)
	state.Put(selfStateKey, process.State)
	if origSelfState != nil {
		defer state.Put(selfStateKey, origSelfState)
	}

	params := s.publishParameters(request, context)
	process.State.Put(paramsStateKey, params)
	if len(workflow.Data) > 0 {
		state := context.State()
		state.Put(dataStateKey, workflow.Data)
		process.State.Put(dataStateKey, workflow.Data)
	}

	upstreamTasks, hasUpstreamTasks := state.GetValue(tasksStateKey)
	restore := context.PublishAndRestore(toolbox.Pairs(
		model.OwnerURL, workflow.Source.URL,
		tasksStateKey, request.Tasks,
	))
	defer restore()

	context.Publish(NewInitEvent(request.Tasks, state))

	taskSelector := model.TasksSelector(request.Tasks)

	if !taskSelector.RunAll() {
		for _, task := range taskSelector.Tasks() {
			if !workflow.TasksNode.Has(task) {
				if hasUpstreamTasks && request.Tasks == toolbox.AsString(upstreamTasks) {
					taskSelector = model.TasksSelector("*")
				} else {
					return nil, fmt.Errorf("failed to lookup task: %v . %v", workflow.Name, task)
				}
			}
		}
	}
	filteredTasks := workflow.TasksNode.Select(taskSelector)
	err = s.runNode(context, "workflow", process, workflow.AbstractNode, func(context *endly.Context, process *model.Process) (in, out data.Map, err error) {
		err = s.runTasks(context, process, filteredTasks)
		return state, response.Data, err
	})

	if len(response.Data) > 0 {
		for k, v := range response.Data {
			upstreamState.Put(k, v)
		}
	}
	return response, err
}

func (s *Service) runNode(context *endly.Context, nodeType string, process *model.Process, node *model.AbstractNode, runHandler func(context *endly.Context, process *model.Process) (in, out data.Map, err error)) error {
	if !process.CanRun() {
		return nil
	}
	original := context.Logging
	context.Logging = node.Logging
	defer func() {
		context.Logging = original
	}()
	var state = context.State()
	canRun, err := criteria.Evaluate(context, context.State(), node.When, node.WhenEval(), fmt.Sprintf("%v.When", nodeType), true)
	if err != nil || !canRun {
		return err
	}
	err = node.Init.Apply(state, state)
	s.addVariableEvent(fmt.Sprintf("%v.Init", nodeType), node.Init, context, state, state)
	if err != nil {
		return err
	}
	in, out, err := runHandler(context, process)
	if err != nil {
		return err
	}
	if len(in) == 0 {
		in = state
	}
	err = node.Post.Apply(in, out)
	s.addVariableEvent(fmt.Sprintf("%v.Post", nodeType), node.Post, context, in, out)
	if err != nil {
		return err
	}
	s.Sleep(context, node.SleepTimeMs)
	return nil
}

func (s *Service) runDeferredTask(context *endly.Context, process *model.Process, parent *model.TasksNode) error {
	if parent.DeferredTask == "" {
		return nil
	}
	task, _ := parent.Task(parent.DeferredTask)
	_, err := s.runTask(context, process, task)
	return err
}

func (s *Service) runOnErrorTask(context *endly.Context, process *model.Process, parent *model.TasksNode, err error) error {
	if parent.OnErrorTask == "" {
		return err
	}
	if err != nil {
		process.Error = err.Error()
		if process.Activity != nil {
			process.Request = process.Activity.Request
			process.Response = process.Activity.Response
			process.TaskName = process.Task.Name
		}
		var state = context.State()
		var processErr = process.AsMap()
		state.Put("error", processErr)
		processErr = toolbox.DeleteEmptyKeys(processErr)
		errorJSON, err := toolbox.AsIndentJSONText(processErr)
		state.Put("errorJSON", errorJSON)
		task, e := parent.Task(parent.OnErrorTask)
		if e != nil {
			return fmt.Errorf("failed to catch: %v, %v", err, e)
		}
		//Reset workflow fail status by default
		if !task.Fail {
			context.Publish(&msg.ResetError{})
		}
		_, err = s.runTask(context, process, task)
		return err
	}
	return err
}

func (s *Service) runTasks(context *endly.Context, process *model.Process, tasks *model.TasksNode) (err error) {
	defer func() {

		e := s.runDeferredTask(context, process, tasks)
		if err == nil {
			err = e
		}
	}()
	for _, task := range tasks.Tasks {
		if task.Name == tasks.OnErrorTask || task.Name == tasks.DeferredTask {
			continue
		}
		if process.IsTerminated() {
			break
		}
		if _, err = s.runTask(context, process, task); err != nil {
			err = s.runOnErrorTask(context, process, tasks, err)
		}
		if err != nil {
			return err
		}
	}
	var scheduledTask = process.Scheduled
	if scheduledTask != nil {
		process.Scheduled = nil
		err = s.runTasks(context, process, &model.TasksNode{Tasks: []*model.Task{scheduledTask}})
	}
	return err
}

func buildParamsMap(request *RunRequest, context *endly.Context) data.Map {
	var params = data.NewMap()
	var state = context.State()
	if len(request.Params) > 0 {
		for k, v := range request.Params {
			params[k] = state.Expand(v)
		}
	}
	return params
}

func (s *Service) startSession(context *endly.Context) bool {
	s.RLock()
	var state = context.State()
	if state.Has(context.SessionID) {
		s.RUnlock()
		return false
	}
	s.RUnlock()
	state.Put(context.SessionID, context)
	s.Lock()
	defer s.Unlock()
	return true
}

func (s *Service) isAsyncRequest(request interface{}) bool {
	if runRequest, ok := request.(*RunRequest); ok {
		return runRequest.Async
	}
	return false
}

func (s *Service) exitWorkflow(context *endly.Context, request *ExitRequest) (*ExitResponse, error) {
	process := Last(context)
	if process != nil {
		process.Terminate()
	}
	return &ExitResponse{}, nil
}

func (s *Service) runGoto(context *endly.Context, request *GotoRequest) (GotoResponse, error) {
	var response interface{}
	process := Last(context)
	if process == nil {
		err := fmt.Errorf("no active workflow")
		return nil, err
	}
	var err error
	var nextTask *model.Task
	nextTask, err = process.Workflow.Task(request.Task)
	if err == nil {
		process.Scheduled = nextTask
	}
	return response, err
}

func getServiceActivity(context *endly.Context) *model.Activity {
	process := Last(context)
	if process == nil {
		return nil
	}
	return process.Activity
}

func getServiceAction(context *endly.Context, actionRequest *model.ServiceRequest) *model.Action {
	activity := getServiceActivity(context)
	var result = actionRequest.NewAction()

	if activity != nil {
		result.MetaTag = activity.MetaTag
		result.Name = activity.Action
		if result.AbstractNode.Description == "" {
			result.AbstractNode.Description = activity.Description
		}
	}
	return result
}

func getSwitchSource(context *endly.Context, sourceKey string) interface{} {
	sourceKey = context.Expand(sourceKey)
	var state = context.State()
	var result = state.Get(sourceKey)
	if result == nil {
		return nil
	}
	return toolbox.DereferenceValue(result)
}

func (s *Service) runSwitch(context *endly.Context, request *SwitchRequest) (SwitchResponse, error) {
	process := LastWorkflow(context)
	if process == nil {
		return nil, errors.New("no active workflow")
	}
	var response interface{}
	var source = getSwitchSource(context, request.SourceKey)
	matched := request.Match(source)
	if matched != nil {
		if matched.Task != "" {
			task, err := process.Workflow.Task(matched.Task)
			if err != nil {
				return nil, err
			}
			return s.runTask(context, process, task)
		}
		serviceAction := getServiceAction(context, matched.ServiceRequest)
		return s.runAction(context, serviceAction, process)

	}
	return response, nil
}

const (
	workflowServiceRunExample = `{
  "Name": "ec2",
  "Params": {
    "awsCredential": "${env.HOME}/.secret/aws-west.json",
    "ec2InstanceId": "i-0139209d5358e60a4"
  },
  "tasks": "start"
}`

	inlineWorkflowServiceRunExample = `{
	"Params": {
		"app": "myapp",
		"appTarget": {
			"Credentials": "localhost",
			"URL": "ssh://127.0.0.1/"
		},
		"buildTarget": {
			"Credentials": "localhost",
			"URL": "ssh://127.0.0.1/"
		},
		"commands": [
			"export GOPATH=/tmp/go",
			"go get -u -v github.com/viant/endly/bootstrap",
			"cd ${buildPath}app",
			"go build -o myapp",
			"chmod +x myapp"
		],
		"download": [
			{
				"Key": "${buildPath}/app/myapp",
				"Value": "$releasePath"
			}
		],
		"origin": [
			{
				"Key": "URL",
				"Value": "./../"
			}
		],
		"sdk": "go:1.9",
		"target": {
			"Credentials": "localhost",
			"URL": "ssh://127.0.0.1/"
		}
	},
	"PublishParameters": true,
	"tasks": "*",
	"URL": "app/build.csv"
}`

	workflowServiceSwitchExample = `{
  "SourceKey": "instanceState",
  "Cases": [
    {
      "Service": "aws/ec2",
      "Action": "call",
      "ServiceRequest": {
        "Credentials": "${env.HOME}/.secret/aws-west.json",
        "Input": {
          "InstanceIds": [
            "i-*********"
          ]
        },
        "Method": "StartInstances"
      },
      "Value": "stopped"
    },
    {
      "Service": "workflow",
      "Action": "exit",
      "Value": "running"
    }
  ]
}
`
	workflowServiceExitExample = `{}`

	workflowServiceGotoExample = `{
		"Task": "stop"
	}`
)

func (s *Service) registerRoutes() {
	s.AbstractService.Register(&endly.Route{
		Action: "run",
		RequestInfo: &endly.ActionInfo{
			Description: "runWorkflow workflow",
			Examples: []*endly.UseCase{
				{
					Description: "run external workflow",
					Data:        workflowServiceRunExample,
				},
				{
					Description: "run inline workflow",
					Data:        inlineWorkflowServiceRunExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &RunRequest{}
		},
		ResponseProvider: func() interface{} {
			return &RunResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*RunRequest); ok {
				return s.run(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "register",
		RequestInfo: &endly.ActionInfo{
			Description: "register workflow",
		},
		RequestProvider: func() interface{} {
			return &RegisterRequest{}
		},
		ResponseProvider: func() interface{} {
			return &LoadResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*RegisterRequest); ok {
				return s.registerWorkflow(req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "switch",
		RequestInfo: &endly.ActionInfo{
			Description: "select action or task for matched case value",
			Examples: []*endly.UseCase{
				{
					Description: "switch case",
					Data:        workflowServiceSwitchExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &SwitchRequest{}
		},
		ResponseProvider: func() interface{} {
			return struct{}{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*SwitchRequest); ok {
				return s.runSwitch(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "goto",
		RequestInfo: &endly.ActionInfo{
			Description: "goto task",
			Examples: []*endly.UseCase{
				{
					Description: "goto",
					Data:        workflowServiceGotoExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &GotoRequest{}
		},
		ResponseProvider: func() interface{} {
			return struct{}{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*GotoRequest); ok {
				return s.runGoto(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "exit",
		RequestInfo: &endly.ActionInfo{
			Description: "exit current workflow",
			Examples: []*endly.UseCase{
				{
					Description: "exit",
					Data:        workflowServiceExitExample,
				},
			},
		},
		RequestProvider: func() interface{} {
			return &ExitRequest{}
		},
		ResponseProvider: func() interface{} {
			return &ExitResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*ExitRequest); ok {
				return s.exitWorkflow(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "setEnv",
		RequestInfo: &endly.ActionInfo{
			Description: "set endly os environment",
		},
		RequestProvider: func() interface{} {
			return &SetEnvRequest{}
		},
		ResponseProvider: func() interface{} {
			return &SetEnvResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*SetEnvRequest); ok {
				return s.setEnv(context, req)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "fail",
		RequestInfo: &endly.ActionInfo{
			Description: "fail workflow execution",
		},
		RequestProvider: func() interface{} {
			return &FailRequest{}
		},
		ResponseProvider: func() interface{} {
			return &FailResponse{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*FailRequest); ok {
				return nil, fmt.Errorf(req.Message)
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "nop",
		RequestInfo: &endly.ActionInfo{
			Description: "iddle operation",
		},
		RequestProvider: func() interface{} {
			return &NopRequest{}
		},
		ResponseProvider: func() interface{} {
			return struct{}{}
		},
		Handler: func(context *endly.Context, request interface{}) (interface{}, error) {
			if req, ok := request.(*NopRequest); ok {
				return req, nil
			}
			return nil, fmt.Errorf("unsupported request type: %T", request)
		},
	})

	s.AbstractService.Register(&endly.Route{
		Action: "print",
		RequestInfo: &endly.ActionInfo{
			Description: "print log message",
		},
		RequestProvider: func() interface{} {
			return &PrintRequest{}
		},
		ResponseProvider: func() interface{} {
			return struct{}{}
		},
		Handler: func(context *endly.Context, req interface{}) (interface{}, error) {
			if request, ok := req.(*PrintRequest); ok {
				if !context.CLIEnabled {
					if request.Message != "" {
						fmt.Printf("%v\n", request.Message)
					}
					if request.Error != "" {
						fmt.Printf("%v\n", request.Error)
					}
				}
				return struct{}{}, nil
			}
			return nil, fmt.Errorf("unsupported request type: %T", req)
		},
	})
}

func (s *Service) setEnv(context *endly.Context, request *SetEnvRequest) (*SetEnvResponse, error) {
	var response = &SetEnvResponse{
		Env: make(map[string]string),
	}
	for _, key := range os.Environ() {
		response.Env[key] = os.Getenv(key)
	}
	if len(request.Env) == 0 {
		return response, nil
	}
	for k, v := range request.Env {
		if err := os.Setenv(k, v); err != nil {
			return nil, err
		}
	}
	return response, nil
}

// New returns a new workflow Service.
func New() endly.Service {
	var result = &Service{

		AbstractService: endly.NewAbstractService(ServiceID),
		fs:              afs.New(),
		registry:        make(map[string]*model.Workflow),
	}
	result.AbstractService.Service = result
	result.registerRoutes()
	return result
}
