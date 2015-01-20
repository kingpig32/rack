package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

var template = `{
  "description": "Convox App",
  "variables": {
    "NAME": null,
    "SOURCE": null,
    "APPCONF": null,
    "BASE_AMI": "ami-447b042c",
    "AWS_REGION": "us-east-1",
    "AWS_ACCESS": "{{env \"AWS_ACCESS\"}}",
    "AWS_SECRET": "{{env \"AWS_SECRET\"}}"
  },
  "builders": [
    {
      "type": "amazon-ebs",
      "region": "{{user \"AWS_REGION\"}}",
      "access_key": "{{user \"AWS_ACCESS\"}}",
      "secret_key": "{{user \"AWS_SECRET\"}}",
      "source_ami": "{{user \"BASE_AMI\"}}",
      "instance_type": "t2.micro",
      "ssh_username": "ubuntu",
      "ami_name": "{{user \"NAME\"}}-{{timestamp}}"
    }
  ],
  "provisioners": [
    {
      "type": "shell",
      "execute_command": "chmod +x {{ .Path }}; {{ .Vars }} sudo -E -S sh '{{ .Path }}'",
      "inline": [
        "mkdir /build",
        "chown ubuntu:ubuntu /build",
        "mkdir /var/app"
      ]
    },
    {
      "type": "file",
      "source": "{{user \"SOURCE\"}}/",
      "destination": "/build"
    },
    {
      "type": "shell",
      "inline": [
        "cd /build",
        "/usr/local/bin/fig -p app build",
        "/usr/local/bin/fig -p app pull"
      ]
    },
    {
      "type": "file",
      "source": "{{user \"APPCONF\"}}",
      "destination": "/tmp/app.conf"
    },
    {
      "type": "shell",
      "execute_command": "chmod +x {{ .Path }}; {{ .Vars }} sudo -E -S sh '{{ .Path }}'",
      "inline": [
        "rm -rf /build",
        "mv /tmp/app.conf /etc/init/app.conf"
      ]
    }
  ]
}`

var appconf = `
start on runlevel [2345]
stop on runlevel [!2345]

respawn

pre-start script
  curl http://169.254.169.254/latest/user-data | jq -r ".start" > /var/app/start
  curl http://169.254.169.254/latest/user-data | jq -r ".env[]" > /var/app/env
  curl http://169.254.169.254/latest/user-data | jq -r '.ports | map("-p \(.):\(.)")[]' | tr '\n' ' ' > /var/app/ports
  curl http://169.254.169.254/latest/user-data | jq -r '.volumes | map("-v \(.)")[]' | tr '\n' ' ' > /var/app/volumes
end script

script
  docker run -a STDOUT -a STDERR --sig-proxy $(cat /var/app/ports) $(cat /var/app/volumes) --env-file /var/app/env app_$(cat /var/app/start)
end script
`

type Builder struct {
	AwsRegion string
	AwsAccess string
	AwsSecret string
}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(repo, name string) error {
	dir, err := ioutil.TempDir("", "repo")

	if err != nil {
		return err
	}

	clone := filepath.Join(dir, "clone")

	cmd := exec.Command("git", "clone", repo, clone)
	cmd.Dir = dir
	cmd.Run()

	data, err := ioutil.ReadFile(filepath.Join(clone, "fig.yml"))

	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		fmt.Printf(",,manifest,,%s\n", scanner.Text())
	}

	return nil

	tf := filepath.Join(dir, "packer.json")
	ioutil.WriteFile(tf, []byte(template), 0644)

	ac := filepath.Join(dir, "app.conf")
	ioutil.WriteFile(ac, []byte(appconf), 0644)

	cmd = exec.Command("packer", "build", "-machine-readable", "-var", "NAME="+name, "-var", "SOURCE="+clone, "-var", "APPCONF="+ac, tf)
	cmd.Stdout = os.Stdout
	cmd.Run()

	return nil
}
