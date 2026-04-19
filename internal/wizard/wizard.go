package wizard

import (
	"errors"
	"strings"

	"github.com/charmbracelet/huh"
)

type ProjectData struct {
	ModuleName   string
	ProjectName  string
	AppName      string
	HasHTTP      bool
	HasGRPC      bool
	HasConsumer  bool
	HasPyroscope bool
	HasMix       bool // true when more than one service selected
}

const defaultName = "my-app"

func Run(prev *ProjectData) (*ProjectData, error) {
	data := defaults(prev)

	services := selectedServices(data)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Directory name for the new project").
				Placeholder("my-app").
				Value(&data.ProjectName),

			huh.NewInput().
				Title("Go module path").
				Description("Module path used in go.mod (e.g. github.com/you/my-app)").
				Placeholder("github.com/you/my-app").
				Value(&data.ModuleName),

			huh.NewInput().
				Title("App name").
				Description("Used for telemetry/tracing identification").
				Placeholder("my-app").
				Value(&data.AppName),
		),
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Services to include").
				Description("Select one or more services").
				Options(
					huh.NewOption("HTTP (Fiber)", "http"),
					huh.NewOption("gRPC", "grpc"),
					huh.NewOption("RabbitMQ Consumer", "consumer"),
				).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return errors.New("select at least one service")
					}
					return nil
				}).
				Value(&services),

			huh.NewConfirm().
				Title("Enable Pyroscope profiling?").
				Value(&data.HasPyroscope),
		),
	)

	if err := form.Run(); err != nil {
		return nil, err
	}

	if data.AppName == "" {
		data.AppName = data.ProjectName
	}
	if data.ModuleName == "" {
		data.ModuleName = "github.com/you/" + data.ProjectName
	}
	data.ModuleName = strings.TrimSpace(data.ModuleName)

	data.HasHTTP, data.HasGRPC, data.HasConsumer = false, false, false
	for _, s := range services {
		switch s {
		case "http":
			data.HasHTTP = true
		case "grpc":
			data.HasGRPC = true
		case "consumer":
			data.HasConsumer = true
		}
	}
	if !data.HasHTTP && !data.HasGRPC && !data.HasConsumer {
		data.HasHTTP = true
	}

	count := 0
	for _, v := range []bool{data.HasHTTP, data.HasGRPC, data.HasConsumer} {
		if v {
			count++
		}
	}
	data.HasMix = count > 1

	return data, nil
}

func defaults(prev *ProjectData) *ProjectData {
	if prev != nil {
		return &ProjectData{
			ProjectName:  prev.ProjectName,
			ModuleName:   prev.ModuleName,
			AppName:      prev.AppName,
			HasHTTP:      prev.HasHTTP,
			HasGRPC:      prev.HasGRPC,
			HasConsumer:  prev.HasConsumer,
			HasPyroscope: prev.HasPyroscope,
		}
	}
	return &ProjectData{
		ProjectName: defaultName,
		ModuleName:  "github.com/you/" + defaultName,
		AppName:     defaultName,
		HasHTTP:     true,
		HasGRPC:     true,
		HasConsumer: true,
	}
}

func selectedServices(data *ProjectData) []string {
	var s []string
	if data.HasHTTP {
		s = append(s, "http")
	}
	if data.HasGRPC {
		s = append(s, "grpc")
	}
	if data.HasConsumer {
		s = append(s, "consumer")
	}
	if len(s) == 0 {
		s = []string{"http", "grpc", "consumer"}
	}
	return s
}
