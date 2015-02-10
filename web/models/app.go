package models

import (
	"fmt"
	"time"

	"github.com/convox/kernel/web/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/convox/kernel/web/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/gen/kinesis"
	"github.com/convox/kernel/web/Godeps/_workspace/src/github.com/goamz/goamz/cloudformation"
)

type App struct {
	Name string

	Status     string
	Outputs    map[string]string
	Parameters map[string]string
	Repository string
	Release    string

	Builds   Builds
	Releases Releases
}

type Apps []App

func ListApps() (Apps, error) {
	res, err := CloudFormation.DescribeStacks("", "")

	if err != nil {
		return nil, err
	}

	apps := make(Apps, 0)

	for _, stack := range res.Stacks {
		tags := stackTags(stack)

		if tags["System"] == "convox" && tags["Type"] == "app" {
			apps = append(apps, *appFromStack(stack))
		}
	}

	return apps, nil
}

func GetApp(name string) (*App, error) {
	res, err := CloudFormation.DescribeStacks(fmt.Sprintf("convox-%s", name), "")

	if err != nil {
		return nil, err
	}

	if len(res.Stacks) != 1 {
		return nil, fmt.Errorf("could not load stack for app: %s", name)
	}

	app := appFromStack(res.Stacks[0])

	app.Outputs = stackOutputs(res.Stacks[0])
	app.Parameters = stackParameters(res.Stacks[0])

	builds, err := ListBuilds(app.Name)

	if err != nil {
		return nil, err
	}

	app.Builds = builds

	releases, err := ListReleases(app.Name)

	if err != nil {
		return nil, err
	}

	app.Releases = releases

	return app, nil
}

func (a *App) Create() error {
	formation, err := a.Formation()

	if err != nil {
		return err
	}

	params := map[string]string{
		"Repository": a.Repository,
	}

	tags := map[string]string{
		"System": "convox",
		"Type":   "app",
	}

	return createStack(formation, fmt.Sprintf("convox-%s", a.Name), params, tags)
}

func (a *App) Delete() error {
	_, err := CloudFormation.DeleteStack(fmt.Sprintf("convox-%s", a.Name))
	return err
}

func (a *App) Formation() (string, error) {
	formation, err := buildFormationTemplate("base", "formation", a)

	if err != nil {
		return "", err
	}

	// printLines(formation)

	return prettyJson(formation)
}

func (a *App) Ami() string {
	release, err := GetRelease(a.Name, a.Release)

	if err != nil {
		return ""
	}

	return release.Ami
}

func (a *App) ProcessFormation() string {
	formation := ""

	for _, p := range a.Processes() {
		env := a.ResourceEnv()

		f, err := p.Formation(env)

		if err != nil {
			panic(err)
		}

		formation += f
	}

	return formation
}

func (a *App) ResourceEnv() string {
	env := ""

	for _, r := range a.Resources() {
		e, err := r.Env()

		if err != nil {
			panic(err)
		}

		env += e
	}

	return env
}

func (a *App) ResourceFormation() string {
	formation := ""

	for _, r := range a.Resources() {
		f, err := r.Formation()

		if err != nil {
			panic(err)
		}

		formation += f
	}

	return formation
}

func (a *App) Subnets() Subnets {
	return ListSubnets()
}

func (a *App) Processes() Processes {
	processes, err := ListProcesses(a.Name)

	if err != nil {
		panic(err)
	}

	return processes
}

func (a *App) Resources() Resources {
	resources, err := ListResources(a.Name)

	if err != nil {
		panic(err)
	}

	return resources
}

func (a *App) Metrics() *Metrics {
	metrics, err := AppMetrics(a.Name)

	if err != nil {
		panic(err)
	}

	return metrics
}

func (a *App) SubscribeLogs(output chan []byte, quit chan bool) error {
	processes := a.Processes()
	done := make([](chan bool), len(processes))

	for i, ps := range processes {
		done[i] = make(chan bool)
		go a.subscribeKinesis(ps.Name, a.Outputs[upperName(ps.Name)+"Kinesis"], output, done[i])
	}

	return nil
}

func (a *App) subscribeKinesis(prefix, stream string, output chan []byte, quit chan bool) {
	sreq := &kinesis.DescribeStreamInput{
		StreamName: aws.String(stream),
	}
	sres, err := Kinesis.DescribeStream(sreq)

	if err != nil {
		fmt.Printf("err1 %+v\n", err)
		// panic(err)
		return
	}

	shards := make([]string, len(sres.StreamDescription.Shards))

	for i, s := range sres.StreamDescription.Shards {
		shards[i] = *s.ShardID
	}

	done := make([](chan bool), len(shards))

	for i, shard := range shards {
		done[i] = make(chan bool)
		go a.subscribeKinesisShard(prefix, stream, shard, output, done[i])
	}
}

func (a *App) subscribeKinesisShard(prefix, stream, shard string, output chan []byte, quit chan bool) {
	ireq := &kinesis.GetShardIteratorInput{
		ShardID:           aws.String(shard),
		ShardIteratorType: aws.String("LATEST"),
		StreamName:        aws.String(stream),
	}
	ires, err := Kinesis.GetShardIterator(ireq)

	if err != nil {
		fmt.Printf("err2 %+v\n", err)
		// panic(err)
		return
	}

	iter := *ires.ShardIterator

	for {
		select {
		case <-quit:
			fmt.Println("quitting")
			return
		default:
			greq := &kinesis.GetRecordsInput{
				ShardIterator: aws.String(iter),
			}
			gres, err := Kinesis.GetRecords(greq)

			if err != nil {
				fmt.Printf("err3 %+v\n", err)
				// panic(err)
				return
			}

			iter = *gres.NextShardIterator

			for _, record := range gres.Records {
				output <- []byte(fmt.Sprintf("%s: %s\n", prefix, string(record.Data)))
			}

			time.Sleep(500 * time.Millisecond)
		}
	}
}

func appFromStack(stack cloudformation.Stack) *App {
	params := stackParameters(stack)

	return &App{
		Name:       stack.StackName[7:],
		Status:     humanStatus(stack.StackStatus),
		Repository: params["Repository"],
		Release:    params["Release"],
	}
}
