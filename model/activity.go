package model

import (
	"github.com/viant/endly"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"time"
)

// Activity represents pipeline or workflow activity
type Activity struct {
	*MetaTag
	Caller          string
	Task            string
	Service         string
	Action          string
	Description     string
	Error           string
	StartTime       time.Time
	Ineligible      bool
	Request         interface{}
	Response        map[string]interface{}
	ServiceResponse *endly.ServiceResponse
	Logging         *bool
}


// FormatTag return a formatted tag
func (a *Activity) FormatTag() string {
	if a.TagIndex != "" {
		return "[" + a.Tag + a.TagIndex + "]"
	}
	return "[" + a.Tag + "]"
}

// NewActivity returns a new workflow Activity.
func NewActivity(context *endly.Context, action *Action, state data.Map) *Activity {
	var result = &Activity{
		Action:  state.ExpandAsText(action.Action),
		Service: state.ExpandAsText(action.Service),
		MetaTag: &MetaTag{
			Tag:            action.Tag,
			TagIndex:       action.TagIndex,
			TagID:          action.TagID,
			TagDescription: action.TagDescription,
			Comments:       action.Comments,
		},
		Description:     context.Expand(action.AbstractNode.Description),
		Request:         action.Request,
		Response:        make(map[string]interface{}),
		StartTime:       time.Now(),
		ServiceResponse: &endly.ServiceResponse{},
	}
	if result.MetaTag == nil {
		result.MetaTag = &MetaTag{}
	}
	if toolbox.IsString(result.Request) {
		result.Request = state.Expand(result.Request)
	}
	if result.Request == nil {
		result.Request = map[string]interface{}{}
	}
	if result.Comments != "" {
		result.Comments = state.ExpandAsText(result.Comments)
	}
	if result.Description != "" {
		result.Description = state.ExpandAsText(result.Description)
	}
	if action.Logging != nil {
		result.Logging = action.Logging
	}
	return result
}

// ActivityEndEvent represents Activity end event type.
type ActivityEndEvent struct {
	Response interface{}
}

// NewActivityEndEvent creates a new ActivityEndEvent
func NewActivityEndEvent(response interface{}) *ActivityEndEvent {
	return &ActivityEndEvent{
		Response: response,
	}
}
