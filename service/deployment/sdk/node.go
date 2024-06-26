package sdk

import (
	"fmt"
	"github.com/viant/endly"
	"github.com/viant/endly/service/deployment/deploy"
	"github.com/viant/endly/internal/util"
	"github.com/viant/endly/model"
	"github.com/viant/endly/service/system/exec"
	"path"
)

type nodeService struct{}

func (s *nodeService) setSdk(context *endly.Context, request *SetRequest) (*Info, error) {
	var result = &Info{}
	var sdkHome = path.Join(request.BaseLocation, "node")
	var runResponse = &exec.RunResponse{}
	var extractRequest = exec.NewExtractRequest(request.Target, exec.DefaultOptions(),
		exec.NewExtractCommand("node -v", "", nil, nil,
			model.NewExtract("version", "v([^\\s]+)", false, false)),
	)
	extractRequest.SystemPaths = append(extractRequest.SystemPaths, fmt.Sprintf("%v/bin", sdkHome))
	if err := endly.Run(context, extractRequest, runResponse); err != nil {
		return nil, err
	}
	var stdout = runResponse.Stdout()
	if util.CheckCommandNotFound(stdout) || util.CheckNoSuchFileOrDirectory(stdout) {
		return nil, errSdkNotFound
	}
	result.Sdk = "node"
	result.Home = sdkHome
	if version, ok := runResponse.Data["version"]; ok {
		result.Version = version.(string)
	}
	if !deploy.MatchVersion(request.Version, result.Version) {
		return nil, errSdkNotFound
	}
	return result, nil
}
