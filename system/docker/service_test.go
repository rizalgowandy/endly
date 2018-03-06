package docker_test

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/viant/endly"
	"github.com/viant/endly/system/docker"
	"github.com/viant/endly/system/exec"
	"github.com/viant/endly/util"
	"github.com/viant/toolbox"
	"github.com/viant/toolbox/data"
	"github.com/viant/toolbox/url"
	"path"
	"strings"
	"testing"
)

func TestDockerService_Images(t *testing.T) {
	var credentialFile, err = util.GetDummyCredential()
	assert.Nil(t, err)

	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir    string
		target     *url.Resource
		Repository string
		Tag        string
		Expected   []*docker.ImageInfo
	}{
		{
			"test/images/darwin",
			target,
			"mysql",
			"5.6",
			[]*docker.ImageInfo{
				{
					Repository: "mysql",
					Tag:        "5.6",
					ImageID:    "96dc914914f5",
					Size:       313524224,
				},
			},
		},
		{
			"test/images/darwin",
			target,
			"",
			"",
			[]*docker.ImageInfo{
				{
					Repository: "mysql",
					Tag:        "5.6",
					ImageID:    "96dc914914f5",
					Size:       313524224,
				},
				{
					Repository: "mysql",
					Tag:        "5.7",
					ImageID:    "5709795eeffa",
					Size:       427819008,
				},
			},
		},
		{
			"test/images/linux",
			target,
			"mysql",
			"5.6",
			[]*docker.ImageInfo{
				{
					Repository: "mysql",
					Tag:        "5.6",
					ImageID:    "96dc914914f5",
					Size:       313524224,
				},
			},
		},
	}

	for _, useCase := range useCases {
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, useCase.target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				var target = useCase.target
				serviceResponse := service.Run(context, &docker.ImagesRequest{
					Target:     target,
					Tag:        useCase.Tag,
					Repository: useCase.Repository,
				})

				var baseCase = useCase.baseDir + " " + useCase.Repository
				assert.Equal(t, "", serviceResponse.Error, baseCase)
				response, ok := serviceResponse.Response.(*docker.ImagesResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}
				if len(response.Images) != len(useCase.Expected) {
					assert.Fail(t, fmt.Sprintf("Expected %v image info but had %v", len(useCase.Expected), len(response.Images)), useCase.baseDir)
				}

				for i, expected := range useCase.Expected {

					if i >= len(response.Images) {
						assert.Fail(t, fmt.Sprintf("Image info was missing [%v] %v", i, baseCase))
						continue
					}
					var actual = response.Images[i]
					assert.Equal(t, expected.Tag, actual.Tag, "Tag "+baseCase)
					assert.EqualValues(t, expected.ImageID, actual.ImageID, "ImageID "+baseCase)
					assert.Equal(t, expected.Repository, actual.Repository, "Repository "+baseCase)
					assert.EqualValues(t, expected.Size, actual.Size, "Size "+baseCase)

				}
			}

		}

	}
}

func TestDockerService_Run(t *testing.T) {

	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)

	mySQLcredentialFile, err := util.GetCredential("mysql", "root", "dev")
	assert.Nil(t, err)

	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir    string
		Request    *docker.RunRequest
		Expected   *docker.ContainerInfo
		TargetName string
		Error      string
	}{
		{
			"test/run/existing/darwin",
			&docker.RunRequest{
				Target: target,
				Image:  "mysql:5.6",
				MappedPort: map[string]string{
					"3306": "3306",
				},
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "**mysql**",
				},
				Mount: map[string]string{
					"/tmp/my.cnf": "/etc/my.cnf",
				},
				Credentials: map[string]string{
					"**mysql**": mySQLcredentialFile,
				},
			},
			&docker.ContainerInfo{
				Status:      "up",
				Names:       "testMysql",
				ContainerID: "83ed7b545cbf",
			},
			"testMysql",
			"",
		},
		{
			"test/run/new/darwin",
			&docker.RunRequest{

				Target: target,
				Image:  "mysql:5.6",
				MappedPort: map[string]string{
					"3306": "3306",
				},
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "**mysql**",
				},
				Mount: map[string]string{
					"/tmp/my.cnf": "/etc/my.cnf",
				},
				Credentials: map[string]string{
					"**mysql**": mySQLcredentialFile,
				},
			},
			&docker.ContainerInfo{
				Status:      "up",
				Names:       "testMysql",
				ContainerID: "98a28566ba7a",
			},
			"testMysql",
			"",
		},
		{
			"test/run/error/darwin",
			&docker.RunRequest{
				Target: target,
				Image:  "mysql:5.6",
				MappedPort: map[string]string{
					"3306": "3306",
				},
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "**mysql**",
				},
				Mount: map[string]string{
					"/tmp/my.cnf": "/etc/my.cnf",
				},
				Credentials: map[string]string{
					"**mysql**": mySQLcredentialFile,
				},
			},
			&docker.ContainerInfo{},
			"testMysql01",
			"error executing docker run --name testMysql01 -e MYSQL_ROOT_PASSWORD=**mysql** -v /tmp/my.cnf:/etc/my.cnf -p 3306:3306  -d mysql:5.6 , c3d9749a1dc43332bb5a58330187719d14c9c23cee55f583cb83bbb3bbb98a80\ndocker: Error response from daemon: driver failed programming external connectivity on endpoint testMysql01 (5c9925d698dfee79f14483fbc42a3837abfb482e30c70e53d830d3d9cfd6f0da): Error starting userland proxy: Bind for 0.0.0.0:3306 failed: port is already allocated.\n",
		},
		{
			"test/run/active/darwin",
			&docker.RunRequest{
				Target: target,
				Image:  "mysql:5.6",
				MappedPort: map[string]string{
					"3306": "3306",
				},
				Env: map[string]string{
					"MYSQL_ROOT_PASSWORD": "**mysql**",
				},
				Mount: map[string]string{
					"/tmp/my.cnf": "/etc/my.cnf",
				},
				Credentials: map[string]string{
					"**mysql**": mySQLcredentialFile,
				},
			},
			&docker.ContainerInfo{
				Status:      "up",
				Names:       "testMysql",
				ContainerID: "84df38a810f7",
			},
			"testMysql",
			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				useCase.Request.Name = useCase.TargetName
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " " + useCase.TargetName

				assert.True(t, strings.Contains(serviceResponse.Error, useCase.Error), baseCase)

				actual, ok := serviceResponse.Response.(*docker.RunResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				if actual == nil {
					continue
				}
				var expected = useCase.Expected
				assert.EqualValues(t, expected.Status, actual.Status, "Status "+baseCase)
				assert.EqualValues(t, expected.Names, actual.Names, "Names "+baseCase)
				assert.EqualValues(t, expected.ContainerID, actual.ContainerID, "ContainerID "+baseCase)
			}

		}

	}
}

func TestDockerService_Command(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)

	mySQLcredentialFile, err := util.GetCredential("mysql", "root", "dev")
	assert.Nil(t, err)

	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir    string
		Request    *docker.ContainerRunRequest
		Expected   string
		TargetName string
		Error      string
	}{
		{
			"test/command/export/darwin",
			&docker.ContainerRunRequest{
				ContainerBaseRequest: &docker.ContainerBaseRequest{
					Target: target,
				},
				Interactive:      true,
				AllocateTerminal: true,
				Command:          "mysqldump  -uroot -p***mysql*** --all-databases --routines | grep -v 'Warning' > /tmp/dump.sql",
				Credentials: map[string]string{
					"***mysql***": mySQLcredentialFile,
				},
			},
			"",
			"testMysql",
			"",
		},
		{
			"test/command/import/darwin",
			&docker.ContainerRunRequest{
				ContainerBaseRequest: &docker.ContainerBaseRequest{
					Target: target,
				},
				Interactive: true,
				Credentials: map[string]string{
					"**mysql**": mySQLcredentialFile,
				},
				Command: "mysql  -uroot -p**mysql** < /tmp/dump.sql",
			},
			"\r\nWarning: Using a password on the command line interface can be insecure.",
			"testMysql",
			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				useCase.Request.Name = useCase.TargetName

				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " " + useCase.TargetName
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)

				actual, ok := serviceResponse.Response.(*docker.ContainerRunResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				if actual == nil {
					continue
				}
				var expected = useCase.Expected
				assert.EqualValues(t, expected, actual.Stdout, "Status "+baseCase)
			}
		}
	}
}

func TestDockerService_Pull(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir  string
		Request  *docker.PullRequest
		Expected *docker.ImageInfo
		Error    string
	}{
		{
			"test/pull/linux",
			&docker.PullRequest{
				Target:     target,
				Repository: "mysql",
				Tag:        "5.7",
			},
			&docker.ImageInfo{
				Repository: "mysql",
				Tag:        "5.7",
				ImageID:    "5709795eeffa",
				Size:       427819008,
			},

			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " "
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)

				actual, ok := serviceResponse.Response.(*docker.PullResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				if actual == nil {
					continue
				}
				var expected = useCase.Expected
				assert.Equal(t, expected.Tag, actual.Tag, "Tag "+baseCase)
				assert.EqualValues(t, expected.ImageID, actual.ImageID, "ImageID "+baseCase)
				assert.Equal(t, expected.Repository, actual.Repository, "Repository "+baseCase)
				assert.EqualValues(t, expected.Size, actual.Size, "Size "+baseCase)

			}

		}

	}

}

func TestDockerService_Status(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //

	var manager = endly.New()
	var useCases = []struct {
		baseDir  string
		Request  *docker.ContainerStatusRequest
		Expected *docker.ContainerStatusResponse
		Error    string
	}{
		{
			"test/status/up/linux",
			&docker.ContainerStatusRequest{
				Target: target,
			},
			&docker.ContainerStatusResponse{
				Containers: []*docker.ContainerInfo{
					{
						ContainerID: "b5bcc949f075",
						Port:        "0.0.0.0:3306->3306/tcp",
						Command:     "docker-entrypoint...",
						Image:       "mysql:5.6",
						Status:      "up",
						Names:       "db1",
					},
				},
			},

			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " "
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)

				response, ok := serviceResponse.Response.(*docker.ContainerStatusResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process resonse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				var expected = useCase.Expected.Containers[0]
				var actual = response.Containers[0]

				assert.Equal(t, expected.ContainerID, actual.ContainerID, "ContainerID "+baseCase)
				assert.EqualValues(t, expected.Port, actual.Port, "Port "+baseCase)
				assert.Equal(t, expected.Command, actual.Command, "Command "+baseCase)
				assert.EqualValues(t, expected.Image, actual.Image, "Image "+baseCase)
				assert.EqualValues(t, expected.Names, actual.Names, "Names "+baseCase)
				assert.EqualValues(t, expected.Status, actual.Status, "Status "+baseCase)

			}

		}

	}

}

func TestDockerService_Start(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir  string
		Request  *docker.ContainerStartRequest
		Expected *docker.ContainerInfo
		Error    string
	}{
		{
			"test/start/linux",
			&docker.ContainerStartRequest{
				ContainerBaseRequest: &docker.ContainerBaseRequest{
					Target: target,
					Name:   "db1",
				},
			},
			&docker.ContainerInfo{
				ContainerID: "b5bcc949f075",
				Port:        "0.0.0.0:3306->3306/tcp",
				Command:     "docker-entrypoint...",
				Image:       "mysql:5.6",
				Status:      "up",
				Names:       "db1",
			},
			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " "
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)

				response, ok := serviceResponse.Response.(*docker.ContainerStartResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				var expected = useCase.Expected
				var actual = response

				assert.Equal(t, expected.ContainerID, actual.ContainerID, "ContainerID "+baseCase)
				assert.EqualValues(t, expected.Port, actual.Port, "Port "+baseCase)
				assert.Equal(t, expected.Command, actual.Command, "Command "+baseCase)
				assert.EqualValues(t, expected.Image, actual.Image, "Image "+baseCase)
				assert.EqualValues(t, expected.Names, actual.Names, "Names "+baseCase)
				assert.EqualValues(t, expected.Status, actual.Status, "Status "+baseCase)

			}

		}

	}

}

func TestDockerService_Stop(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	var manager = endly.New()
	var useCases = []struct {
		baseDir  string
		Request  *docker.ContainerStopRequest
		Expected *docker.ContainerInfo
		Error    string
	}{
		{
			"test/stop/linux",
			&docker.ContainerStopRequest{
				ContainerBaseRequest: &docker.ContainerBaseRequest{
					Target: target,
					Name:   "db1",
				},
			},
			&docker.ContainerInfo{
				ContainerID: "b5bcc949f075",
				Port:        "0.0.0.0:3306->3306/tcp",
				Command:     "docker-entrypoint...",
				Image:       "mysql:5.6",
				Status:      "down",
				Names:       "db1",
			},
			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " "
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)
				response, ok := serviceResponse.Response.(*docker.ContainerStopResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				var expected = useCase.Expected
				var actual = response

				assert.Equal(t, expected.ContainerID, actual.ContainerID, "ContainerID "+baseCase)
				assert.EqualValues(t, expected.Port, actual.Port, "Port "+baseCase)
				assert.Equal(t, expected.Command, actual.Command, "Command "+baseCase)
				assert.EqualValues(t, expected.Image, actual.Image, "Image "+baseCase)
				assert.EqualValues(t, expected.Names, actual.Names, "Names "+baseCase)
				assert.EqualValues(t, expected.Status, actual.Status, "Status "+baseCase)
			}
		}
	}
}

func TestDockerService_Remove(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //

	var manager = endly.New()
	var useCases = []struct {
		baseDir  string
		Request  *docker.ContainerRemoveRequest
		Expected string
		Error    string
	}{
		{
			"test/remove/linux",
			&docker.ContainerRemoveRequest{
				ContainerBaseRequest: &docker.ContainerBaseRequest{
					Target: target,
					Name:   "db1",
				},
			},
			"db1",
			"",
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			assert.Nil(t, err)

			defer context.Close()
			if assert.Nil(t, err) {
				serviceResponse := service.Run(context, useCase.Request)

				var baseCase = useCase.baseDir + " "
				assert.Equal(t, useCase.Error, serviceResponse.Error, baseCase)

				response, ok := serviceResponse.Response.(*docker.ContainerRemoveResponse)
				if !ok {
					assert.Fail(t, fmt.Sprintf("process serviceResponse was empty %v %T", baseCase, serviceResponse.Response))
					continue
				}

				var expected = useCase.Expected
				var actual = response
				assert.Equal(t, expected, actual.Stdout, "Command "+baseCase)

			}

		}

	}
}

func TestDockerService_Login(t *testing.T) {

	parent := toolbox.CallerDirectory(3)
	gcrKeyDockerCredentials := path.Join(parent, "test/gcr_key.json")
	keyDockerCredentials := path.Join(parent, "test/key.json")

	credentialFile, err := util.GetDummyCredential()
	assert.Nil(t, err)
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //

	var manager = endly.New()
	var useCases = []struct {
		baseDir          string
		Request          *docker.LoginRequest
		ExpectedUserName string
		ExpectedStdout   string
		Error            bool
	}{
		{
			"test/login/gcr_key/darwin",
			&docker.LoginRequest{

				Target:     target,
				Repository: "us.gcr.io/myproj",
				Credential: gcrKeyDockerCredentials,
			},
			"_json_key",
			"Login Succeeded",
			false,
		},
		//{
		//	"test/login/gcr/darwin",
		//	&docker.LoginRequest{
		//
		//		Target:     target,
		//		Repository: "us.gcr.io/myproj",
		//		Credential: keyDockerCredentials,
		//	},
		//	"oauth2accesstoken",
		//	"Login Succeeded",
		//	false,
		//},
		{
			"test/login/std/darwin",
			&docker.LoginRequest{

				Target:     target,
				Repository: "repo.com/myproj",
				Credential: keyDockerCredentials,
			},
			"",
			"",
			true,
		},
	}

	for _, useCase := range useCases {
		var target = useCase.Request.Target
		execService, err := exec.GetReplayService(useCase.baseDir)
		if assert.Nil(t, err) {
			context, err := exec.OpenTestContext(manager, target, execService)
			service, err := context.Service(docker.ServiceID)
			defer context.Close()

			if assert.Nil(t, err) {

				serviceResponse := service.Run(context, useCase.Request)

				if useCase.Error {
					assert.True(t, serviceResponse.Error != "")
					serviceResponse = service.Run(context, &docker.LogoutRequest{
						Target:     useCase.Request.Target,
						Repository: useCase.Request.Repository,
					})
					continue
				}
				if assert.EqualValues(t, "", serviceResponse.Error, useCase.baseDir) {
					response, ok := serviceResponse.Response.(*docker.LoginResponse)
					if assert.True(t, ok) {
						assert.EqualValues(t, useCase.ExpectedUserName, response.Username)
						assert.EqualValues(t, useCase.ExpectedStdout, response.Stdout)
					}
					serviceResponse = service.Run(context, &docker.LogoutRequest{
						Target:     useCase.Request.Target,
						Repository: useCase.Request.Repository,
					})
					assert.EqualValues(t, "", serviceResponse.Error)
				}
			}
		}
	}

}

func TestDockerService_Build(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	manager := endly.New()

	execService, err := exec.GetReplayService("test/build/darwin")
	if !assert.Nil(t, err) {
		return
	}
	context, err := exec.OpenTestContext(manager, target, execService)
	if !assert.Nil(t, err) {
		return
	}
	defer context.Close()
	service, _ := context.Service(docker.ServiceID)

	response := service.Run(context, &docker.BuildRequest{
		Target: target,
		Tag: &docker.Tag{
			Username: "viant",
			Image:    "site_profile_backup",
			Version:  "0.1",
		},
		Path: "/Users/awitas/go/src/github.vianttech.com/etl/site_profile_backup",
	})
	assert.EqualValues(t, "", response.Error)

}

func TestDockerService_Push(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	if err != nil {
		return
	}
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	manager := endly.New()
	var useCases = []struct {
		baseDir string
		Error   bool
	}{
		{
			baseDir: "test/push/error/darwin",
			Error:   true,
		},
		{
			baseDir: "test/push/success/darwin",
			Error:   false,
		},
	}

	for _, useCase := range useCases {
		execService, err := exec.GetReplayService(useCase.baseDir)
		if !assert.Nil(t, err, useCase.baseDir) {
			return
		}
		context, err := exec.OpenTestContext(manager, target, execService)
		if !assert.Nil(t, err) {
			return
		}
		defer context.Close()
		service, _ := context.Service(docker.ServiceID)

		response := service.Run(context, &docker.PushRequest{
			Target: target,
			Tag: &docker.Tag{
				Username: "viant",
				Image:    "site_profile_backup",
				Version:  "0.1",
			},
		})
		if useCase.Error {
			assert.True(t, response.Error != "")
		} else {
			assert.EqualValues(t, "", response.Error)

		}
	}
}

func TestDockerService_Inspect(t *testing.T) {
	credentialFile, err := util.GetDummyCredential()
	if err != nil {
		return
	}
	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
	manager := endly.New()
	execService, err := exec.GetReplayService("test/inspect/darwin")
	context, err := exec.OpenTestContext(manager, target, execService)
	if !assert.Nil(t, err) {
		return
	}
	defer context.Close()
	service, _ := context.Service(docker.ServiceID)
	serviceResponse := service.Run(context, &docker.ContainerInspectRequest{
		ContainerBaseRequest: &docker.ContainerBaseRequest{
			Target: target,
			Name:   "site_backup",
		},
	})
	assert.EqualValues(t, "", serviceResponse.Error)
	response, ok := serviceResponse.Response.(*docker.ContainerInspectResponse)
	if assert.True(t, ok) {
		if assert.True(t, response.Stdout != "") {
			assert.NotNil(t, response.Info)
			var aMap = data.NewMap()
			aMap.Put("Output", toolbox.AsSlice(response.Info))
			ip, has := aMap.GetValue("Output[0].NetworkSettings.IPAddress")
			if assert.True(t, has) {
				assert.EqualValues(t, "172.17.0.2", ip)
			}
		}
	}
}

//
//func TestDockerService_RecorderTemplate(t *testing.T) {
//
//	parent := toolbox.CallerDirectory(3)
//	dockerCredentials := path.Join(parent, "test/gcr_key.json")
//
//	var credentialFile = path.Join(os.Getenv("HOME"), ".secret/localhost.json")
//
//	var target = url.NewResource("scp://127.0.0.1:22/", credentialFile) //
//	manager := endly.New()
//	context, _ := OpenTestRecorderContext(manager, target, "test/inspect/darwin")
//	///context := manager.NewContext(toolbox.NewContext())
//
//	defer context.Close()
//
//	service, _ := manager.Service(endly.ID)
//
//	fmt.Printf("%v\n", dockerCredentials)
//
//	target.Name = "site_backup"
//	response := service.Run(context, &docker.ContainerInspectRequest{
//		Target:     target,
//
//	})
//	assert.EqualValues(t, "", response.Error)
//
//
//}
//

func TestDockerLoginRequest_Validate(t *testing.T) {
	{
		request := &docker.LoginRequest{}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.LoginRequest{
			Target: url.NewResource("abc"),
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.LoginRequest{
			Repository: "abc",
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.LoginRequest{
			Repository: "abc",
			Target:     url.NewResource("abc"),
		}
		assert.Nil(t, request.Validate())
	}
}

func Test_DockerBuildRequest_Validate(t *testing.T) {
	{
		request := docker.BuildRequest{}
		assert.NotNil(t, request.Validate())
	}
	{
		request := docker.BuildRequest{Target: url.NewResource("abc"), Tag: &docker.Tag{}}
		assert.NotNil(t, request.Validate())
	}
	{
		request := docker.BuildRequest{Target: url.NewResource("abc"),
			Arguments: map[string]string{
				"-t": "image:1.0",
			},
			Path: "/",
			Tag:  &docker.Tag{Image: "abc"}}
		assert.Nil(t, request.Validate())
	}

	{
		request := docker.BuildRequest{Target: url.NewResource("abc"),
			Path: "/",
			Tag:  &docker.Tag{Image: "abc"}}
		assert.Nil(t, request.Validate())
	}
	{
		request := docker.BuildRequest{Target: url.NewResource("abc"),

			Tag: &docker.Tag{Image: "abc"}}
		assert.NotNil(t, request.Validate())
	}
}

func TestDockerTag_Validate(t *testing.T) {

	{
		request := &docker.TagRequest{}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.TagRequest{
			Target: url.NewResource("abc"),
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.TagRequest{
			Target:    url.NewResource("abc"),
			SourceTag: &docker.Tag{},
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.TagRequest{
			Target:    url.NewResource("abc"),
			SourceTag: &docker.Tag{},
			TargetTag: &docker.Tag{},
		}
		assert.NotNil(t, request.Validate())
	}

	{
		request := &docker.TagRequest{
			Target:    url.NewResource("abc"),
			SourceTag: &docker.Tag{},
			TargetTag: &docker.Tag{
				Image: "abc",
			},
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.TagRequest{
			Target: url.NewResource("abc"),
			SourceTag: &docker.Tag{
				Image: "abc",
			},
			TargetTag: &docker.Tag{},
		}
		assert.NotNil(t, request.Validate())
	}
	{
		request := &docker.TagRequest{
			Target: url.NewResource("abc"),
			SourceTag: &docker.Tag{
				Image: "abc",
			},
			TargetTag: &docker.Tag{
				Image: "abc",
			},
		}
		assert.Nil(t, request.Validate())
	}

}

func TestDockerTag_String(t *testing.T) {
	{
		tag := &docker.Tag{
			Image: "abc",
		}
		assert.EqualValues(t, "abc", tag.String())
	}
	{
		tag := &docker.Tag{
			Image:   "abc",
			Version: "latest",
		}
		assert.EqualValues(t, "abc:latest", tag.String())
	}
	{
		tag := &docker.Tag{
			Registry: "reg.org",
			Image:    "abc",
			Version:  "latest",
		}
		assert.EqualValues(t, "reg.org/abc:latest", tag.String())
	}
	{
		tag := &docker.Tag{
			Username: "reg.org",
			Image:    "abc",
			Version:  "latest",
		}
		assert.EqualValues(t, "reg.org/abc:latest", tag.String())
	}
}
