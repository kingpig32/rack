package main

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/convox/cli/Godeps/_workspace/src/github.com/codegangsta/cli"
	"github.com/convox/cli/Godeps/_workspace/src/golang.org/x/crypto/ssh/terminal"
	"github.com/convox/cli/stdcli"
)

func init() {
	stdcli.RegisterCommand(cli.Command{
		Name:        "login",
		Description: "login to your convox installation",
		Usage:       "<host>",
		Action:      cmdLogin,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "password",
				Usage: "password to use for authentication. If not specified, prompt for password.",
			},
			cli.BoolFlag{
				Name:  "boot2docker",
				Usage: "configure boot2docker for an insecure registry (development).",
			},
		},
	})
}

func cmdLogin(c *cli.Context) {
	if len(c.Args()) != 1 {
		stdcli.Usage(c, "login")
	}

	host := c.Args()[0]
	u, err := url.Parse(host)

	if err != nil {
		stdcli.Error(err)
		return
	}

	if u.Host != "" {
		host = u.Host
	}

	password := c.String("password")

	if password == "" {
		fmt.Print("Password: ")

		in, err := terminal.ReadPassword(int(os.Stdin.Fd()))

		fmt.Println()

		if err != nil {
			stdcli.Error(err)
			return
		}

		password = string(in)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("https://%s/apps", host), nil)

	if err != nil {
		stdcli.Error(err)
		return
	}

	req.SetBasicAuth("convox", string(password))

	res, err := client.Do(req)

	if err != nil {
		stdcli.Error(err)
		return
	}

	defer res.Body.Close()

	if res.StatusCode != 200 {
		stdcli.Error(fmt.Errorf("invalid login"))
		return
	}

	config, err := configFile()

	if err != nil {
		stdcli.Error(err)
		return
	}

	err = ioutil.WriteFile(config, []byte(fmt.Sprintf("%s\n%s\n", host, password)), 0600)

	if err != nil {
		stdcli.Error(err)
		return
	}

	fmt.Println("Login Succeeded")

	if c.Bool("boot2docker") {
		// Log into private registry
		stdcli.Run(
			"boot2docker",
			"ssh",
			fmt.Sprintf("echo $'EXTRA_ARGS=\"--insecure-registry %s:5000\"' | sudo tee -a /var/lib/boot2docker/profile && sudo /etc/init.d/docker restart", host),
		)
		stdcli.Run("docker", "login", "-e", "user@convox.io", "-u", "convox", "-p", password, host+":5000")
	}
}

func configFile() (string, error) {
	u, err := user.Current()

	if err != nil {
		return "", err
	}

	return filepath.Join(u.HomeDir, ".convox"), nil
}

func currentLogin() (string, string, error) {
	if os.Getenv("CONSOLE_HOST") != "" && os.Getenv("REGISTRY_PASSWORD") != "" {
		return os.Getenv("CONSOLE_HOST"), os.Getenv("REGISTRY_PASSWORD"), nil
	}

	config, err := configFile()

	if err != nil {
		return "", "", err
	}

	if !exists(config) {
		stdcli.Error(fmt.Errorf("must login first"))
		return "", "", err
	}

	data, err := ioutil.ReadFile(config)

	if err != nil {
		return "", "", err
	}

	parts := strings.Split(string(data), "\n")

	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid config")
	}

	return parts[0], parts[1], nil
}
