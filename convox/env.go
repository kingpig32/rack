package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/convox/cli/Godeps/_workspace/src/github.com/codegangsta/cli"
	"github.com/convox/cli/stdcli"
)

func init() {
	stdcli.RegisterCommand(cli.Command{
		Name:        "env",
		Description: "manage an app's environment variables",
		Usage:       "get|set|unset",
		Action:      cmdEnvGet,
		Subcommands: []cli.Command{
			{
				Name:   "get",
				Usage:  "VARIABLE",
				Action: cmdEnvGet,
			},
			{
				Name:   "set",
				Usage:  "VARIABLE=VALUE",
				Action: cmdEnvSet,
			},
			{
				Name:   "unset",
				Usage:  "VARIABLE",
				Action: cmdEnvUnset,
			},
		},
	})
}

func cmdEnvGet(c *cli.Context) {
	appName := dir()

	fmt.Println(fetchEnv(appName))
}

func cmdEnvSet(c *cli.Context) {
	appName := dir()

	var old map[string]string
	json.Unmarshal(fetchEnv(appName), &old)

	data := ""

	for key, value := range old {
		data += fmt.Sprintf("%s=%s\n", key, value)
	}

	for _, value := range c.Args() {
		data += fmt.Sprintf("%s\n", value)
	}

	path := fmt.Sprintf("/apps/%s/environment", appName)

	resp, err := ConvoxPost(path, data)

	if err != nil {
		stdcli.Error(err)
		return
	}

	fmt.Println(string(resp[:]))
}

func cmdEnvUnset(c *cli.Context) {
	variable := c.Args()[0]

	appName := dir()

	path := fmt.Sprintf("/apps/%s/environment/%s", appName, variable)

	resp, err := ConvoxDelete(path)

	if err != nil {
		stdcli.Error(err)
		return
	}

	fmt.Println(string(resp[:]))
}

func dir() string {
	wd, err := os.Getwd()

	if err != nil {
		panic(err)
	}

	return path.Base(wd)
}

func fetchEnv(app string) []byte {
	appName := dir()
	path := fmt.Sprintf("/apps/%s/environment", appName)

	resp, err := ConvoxGet(path)

	if err != nil {
		panic(err)
	}

	return resp
}
