package formation

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/convox/kernel/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/aws"
	"github.com/convox/kernel/Godeps/_workspace/src/github.com/awslabs/aws-sdk-go/service/ecs"
	"github.com/convox/kernel/models"
)

func HandleECSService(req Request) (string, error) {
	switch req.RequestType {
	case "Create":
		fmt.Println("CREATING SERVICE")
		fmt.Printf("req %+v\n", req)
		return ECSServiceCreate(req)
	case "Update":
		fmt.Println("UPDATING SERVICE")
		fmt.Printf("req %+v\n", req)
		return ECSServiceUpdate(req)
	case "Delete":
		fmt.Println("DELETING SERVICE")
		fmt.Printf("req %+v\n", req)
		return ECSServiceDelete(req)
	}

	return "", fmt.Errorf("unknown RequestType: %s", req.RequestType)
}

func HandleECSTaskDefinition(req Request) (string, error) {
	switch req.RequestType {
	case "Create":
		fmt.Println("CREATING TASK")
		fmt.Printf("req %+v\n", req)
		return ECSTaskDefinitionCreate(req)
	case "Update":
		fmt.Println("UPDATING TASK")
		fmt.Printf("req %+v\n", req)
		return ECSTaskDefinitionCreate(req)
	case "Delete":
		fmt.Println("DELETING TASK")
		fmt.Printf("req %+v\n", req)
		return ECSTaskDefinitionDelete(req)
	}

	return "", fmt.Errorf("unknown RequestType: %s", req.RequestType)
}

func ECSServiceCreate(req Request) (string, error) {
	count, err := strconv.Atoi(req.ResourceProperties["DesiredCount"].(string))

	if err != nil {
		return "", err
	}

	r := &ecs.CreateServiceInput{
		Cluster:        aws.String(req.ResourceProperties["Cluster"].(string)),
		DesiredCount:   aws.Long(int64(count)),
		ServiceName:    aws.String(req.ResourceProperties["Name"].(string)),
		TaskDefinition: aws.String(req.ResourceProperties["TaskDefinition"].(string)),
	}

	balancers := req.ResourceProperties["LoadBalancers"].([]interface{})

	if len(balancers) > 0 {
		r.Role = aws.String(req.ResourceProperties["Role"].(string))
	}

	for _, balancer := range balancers {
		parts := strings.SplitN(balancer.(string), ":", 3)

		if len(parts) != 3 {
			return "", fmt.Errorf("invalid load balancer specification: %s", balancer.(string))
		}

		name := parts[0]
		ps := parts[1]
		port, _ := strconv.Atoi(parts[2])

		r.LoadBalancers = append(r.LoadBalancers, &ecs.LoadBalancer{
			LoadBalancerName: aws.String(name),
			ContainerName:    aws.String(ps),
			ContainerPort:    aws.Long(int64(port)),
		})

		break
	}

	res, err := ECS().CreateService(r)

	if err != nil {
		return "", err
	}

	return *res.Service.ServiceARN, nil
}

func ECSServiceUpdate(req Request) (string, error) {
	count, _ := strconv.Atoi(req.ResourceProperties["DesiredCount"].(string))

	res, err := ECS().UpdateService(&ecs.UpdateServiceInput{
		Cluster:        aws.String(req.ResourceProperties["Cluster"].(string)),
		Service:        aws.String(req.ResourceProperties["Name"].(string)),
		DesiredCount:   aws.Long(int64(count)),
		TaskDefinition: aws.String(req.ResourceProperties["TaskDefinition"].(string)),
	})

	if err != nil {
		return "", err
	}

	return *res.Service.ServiceARN, nil
}

func ECSServiceDelete(req Request) (string, error) {
	cluster := req.ResourceProperties["Cluster"].(string)
	name := req.ResourceProperties["Name"].(string)

	_, err := ECS().UpdateService(&ecs.UpdateServiceInput{
		Cluster:      aws.String(cluster),
		Service:      aws.String(name),
		DesiredCount: aws.Long(0),
	})

	// go ahead and mark the delete good if the service is not found
	if ae, ok := err.(aws.APIError); ok {
		if ae.Code == "ServiceNotFoundException" {
			return "", nil
		}
	}

	// TODO let the cloudformation finish thinking this deleted
	// but take note so we can figure out why
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return "", nil
	}

	_, err = ECS().DeleteService(&ecs.DeleteServiceInput{
		Cluster: aws.String(cluster),
		Service: aws.String(name),
	})

	// TODO let the cloudformation finish thinking this deleted
	// but take note so we can figure out why
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return "", nil
	}

	return "", nil
}

func ECSTaskDefinitionCreate(req Request) (string, error) {
	// return "", fmt.Errorf("fail")

	tasks := req.ResourceProperties["Tasks"].([]interface{})

	r := &ecs.RegisterTaskDefinitionInput{
		Family: aws.String(req.ResourceProperties["Name"].(string)),
	}

	// download environment
	var env models.Environment

	if envUrl := req.ResourceProperties["Environment"].(string); envUrl != "" {
		res, err := http.Get(envUrl)

		if err != nil {
			return "", err
		}

		defer res.Body.Close()

		data, err := ioutil.ReadAll(res.Body)

		env = models.LoadEnvironment(data)
	}

	r.ContainerDefinitions = make([]*ecs.ContainerDefinition, len(tasks))

	for i, itask := range tasks {
		task := itask.(map[string]interface{})

		cpu, _ := strconv.Atoi(task["CPU"].(string))
		memory, _ := strconv.Atoi(task["Memory"].(string))

		r.ContainerDefinitions[i] = &ecs.ContainerDefinition{
			Name:      aws.String(task["Name"].(string)),
			Essential: aws.Boolean(true),
			Image:     aws.String(task["Image"].(string)),
			CPU:       aws.Long(int64(cpu)),
			Memory:    aws.Long(int64(memory)),
		}

		if command := task["Command"].(string); command != "" {
			r.ContainerDefinitions[i].Command = []*string{aws.String("sh"), aws.String("-c"), aws.String(command)}
		}

		// set environment
		for key, val := range env {
			r.ContainerDefinitions[i].Environment = append(r.ContainerDefinitions[0].Environment, &ecs.KeyValuePair{
				Name:  aws.String(key),
				Value: aws.String(val),
			})
		}

		// set links
		if task["Links"] != nil {
			links := task["Links"].([]interface{})

			r.ContainerDefinitions[i].Links = make([]*string, len(links))

			for j, link := range links {
				r.ContainerDefinitions[i].Links[j] = aws.String(link.(string))
			}
		}

		// set portmappings
		ports := task["PortMappings"].([]interface{})

		r.ContainerDefinitions[i].PortMappings = make([]*ecs.PortMapping, len(ports))

		for j, port := range ports {
			parts := strings.Split(port.(string), ":")
			host, _ := strconv.Atoi(parts[0])
			container, _ := strconv.Atoi(parts[1])

			r.ContainerDefinitions[i].PortMappings[j] = &ecs.PortMapping{
				ContainerPort: aws.Long(int64(container)),
				HostPort:      aws.Long(int64(host)),
			}
		}

	}

	res, err := ECS().RegisterTaskDefinition(r)

	if err != nil {
		return "", err
	}

	return *res.TaskDefinition.TaskDefinitionARN, nil
}

func ECSTaskDefinitionDelete(req Request) (string, error) {
	// TODO: currently unsupported by ECS
	// res, err := ECS().DeregisterTaskDefinition(&ecs.DeregisterTaskDefinitionInput{TaskDefinition: aws.String(req.PhysicalResourceId)})
	return "", nil
}
