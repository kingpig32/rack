package models

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/convox/rack/Godeps/_workspace/src/github.com/aws/aws-sdk-go/aws"
	"github.com/convox/rack/Godeps/_workspace/src/github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/convox/rack/Godeps/_workspace/src/github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/convox/rack/client"
)

type System client.System

var DescribeStacksCache = map[string]DescribeStacksResult{}

var DescribeStacksCacheTTL = 5 * time.Second

var DescribeStacksMutex = &sync.Mutex{}

type DescribeStacksResult struct {
	Name        string
	Output      *cloudformation.DescribeStacksOutput
	RequestTime time.Time
}

func DescribeStacks() (*cloudformation.DescribeStacksOutput, error) {
	return doDescribeStack(cloudformation.DescribeStacksInput{})
}

func DescribeStack(name string) (*cloudformation.DescribeStacksOutput, error) {
	return doDescribeStack(cloudformation.DescribeStacksInput{
		StackName: aws.String(name),
	})
}

func UpdateStack(req *cloudformation.UpdateStackInput) (*cloudformation.UpdateStackOutput, error) {
	if req.StackName != nil {
		name := *req.StackName
		fmt.Printf("fn=UpdateStack at=delete name=%q\n", name)

		delete(DescribeStacksCache, name)
	}

	return CloudFormation().UpdateStack(req)
}

func doDescribeStack(input cloudformation.DescribeStacksInput) (*cloudformation.DescribeStacksOutput, error) {
	DescribeStacksMutex.Lock()
	defer DescribeStacksMutex.Unlock()

	name := "<blank>"

	if input.StackName != nil {
		name = *input.StackName
	}

	s := DescribeStacksCache[name]

	// if last request was before the TTL, or if running in the test environment, make a request
	if s.RequestTime.Before(time.Now().Add(-DescribeStacksCacheTTL)) || os.Getenv("AWS_REGION") == "test" {
		fmt.Printf("fn=doDescribeStack at=miss name=%q age=%s\n", name, time.Now().Sub(s.RequestTime))

		res, err := CloudFormation().DescribeStacks(&input)

		if err == nil {
			DescribeStacksCache[name] = DescribeStacksResult{
				Name:        name,
				Output:      res,
				RequestTime: time.Now(),
			}
		}

		return res, err
	}

	fmt.Printf("fn=doDescribeStack at=hit name=%q age=%s\n", name, time.Now().Sub(s.RequestTime))

	return s.Output, nil
}

func GetSystem() (*System, error) {
	rack := os.Getenv("RACK")

	res, err := DescribeStack(rack)

	if err != nil {
		return nil, err
	}

	if len(res.Stacks) != 1 {
		return nil, fmt.Errorf("could not load stack for app: %s", rack)
	}

	stack := res.Stacks[0]
	params := stackParameters(stack)

	count, err := strconv.Atoi(params["InstanceCount"])

	if err != nil {
		return nil, err
	}

	r := &System{
		Count:   count,
		Name:    rack,
		Status:  humanStatus(*stack.StackStatus),
		Type:    params["InstanceType"],
		Version: os.Getenv("RELEASE"),
	}

	return r, nil
}

func (r *System) Save() error {
	rack := os.Getenv("RACK")

	app, err := GetApp(rack)

	if err != nil {
		return err
	}

	// Validate scale
	mac, err := maxAppConcurrency()

	// dont scale the rack below the max concurrency plus one
	// see formation.go for more details
	if err == nil && r.Count < (mac+1) {
		return fmt.Errorf("max process concurrency is %d, can't scale rack below %d instances", mac, mac+1)
	}

	// Read new formation template and parameters
	url := fmt.Sprintf("https://convox.s3.amazonaws.com/release/%s/formation.json", r.Version)

	resp, err := http.Get(url)

	if err != nil {
		return err
	}

	defer resp.Body.Close()

	formation, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return err
	}

	existing, err := formationParameters(string(formation))

	if err != nil {
		return err
	}

	// set new parameters
	newVersion := r.Version != app.Parameters["Version"]

	app.Parameters["InstanceCount"] = strconv.Itoa(r.Count)
	app.Parameters["InstanceType"] = r.Type
	app.Parameters["Version"] = r.Version

	params := []*cloudformation.Parameter{}

	// filter out parameters removed from the template
	for key, value := range app.Parameters {
		if _, ok := existing[key]; ok {
			params = append(params, &cloudformation.Parameter{ParameterKey: aws.String(key), ParameterValue: aws.String(value)})
		}
	}

	// update the stack
	req := &cloudformation.UpdateStackInput{
		Capabilities: []*string{aws.String("CAPABILITY_IAM")},
		StackName:    aws.String(rack),
		TemplateURL:  aws.String(url),
		Parameters:   params,
	}

	_, err = UpdateStack(req)

	if err != nil {
		return err
	}

	// save a record of the new release
	if newVersion {
		req := &dynamodb.PutItemInput{
			Item: map[string]*dynamodb.AttributeValue{
				"id":      &dynamodb.AttributeValue{S: aws.String(r.Version)},
				"app":     &dynamodb.AttributeValue{S: aws.String(rack)},
				"created": &dynamodb.AttributeValue{S: aws.String(time.Now().Format(SortableTime))},
			},
			TableName: aws.String(releasesTable(rack)),
		}

		_, err = DynamoDB().PutItem(req)

		if err != nil {
			return err
		}
	}

	return nil
}

func maxAppConcurrency() (int, error) {
	apps, err := ListApps()

	if err != nil {
		return 0, err
	}

	max := 0

	for _, app := range apps {
		rel, err := app.LatestRelease()

		if err != nil {
			return 0, err
		}

		if rel == nil {
			continue
		}

		m, err := LoadManifest(rel.Manifest)

		if err != nil {
			return 0, err
		}

		f, err := ListFormation(app.Name)

		if err != nil {
			return 0, err
		}

		for _, me := range m {
			if len(me.ExternalPorts()) > 0 {
				entry := f.Entry(me.Name)

				if entry != nil && entry.Count > max {
					max = entry.Count
				}
			}
		}
	}

	return max, nil
}
