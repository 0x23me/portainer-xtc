package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/portainer/portainer/api/http/client"
	"github.com/rjeczalik/notify"
	"github.com/spf13/pflag"
)

const (
	PortainerHTTPTimeout = 120 * time.Second
)

func main() {
	if err := Run(); err != nil {
		panic(err)
	}
	os.Exit(0)
}

type StacksResponse []Stack

type Stack struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	EndpointId int    `json:"endpointId"`
}

type Endpoints []Endpoint

type Endpoint struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Config holds all configuration for the application.
type Config struct {
	PortainerAddress string
	APIKey           string
	StackFilesDir    string
	WatchStackdsDir  bool
}

func NewConfigFromFlags() Config {
	cfg := DefaultConfig

	pflag.StringVar(&cfg.PortainerAddress, "portainer-address", cfg.PortainerAddress, "Address of Portainer api")
	pflag.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "API Key")
	pflag.StringVar(&cfg.StackFilesDir, "stack-files-dir", cfg.StackFilesDir, "Directory to stack files")
	pflag.BoolVarP(&cfg.WatchStackdsDir, "watch-stacks-dir", "w", cfg.WatchStackdsDir, "Watch stacks dir")

	pflag.Parse()

	if !strings.HasSuffix(cfg.StackFilesDir, "/") {
		cfg.StackFilesDir += "/"
	}

	return cfg
}

func (c *Config) EndpointsURL() string {
	return fmt.Sprintf("%s/api/%s", c.PortainerAddress, "endpoints")
}

func (c *Config) StacksURL() string {
	return fmt.Sprintf("%s/api/%s", c.PortainerAddress, "stacks")
}

// DefaultConfig is a configuration with default values.
var DefaultConfig = Config{
	PortainerAddress: "",
	APIKey:           "",
	StackFilesDir:    "./stacks/",
	WatchStackdsDir:  false,
}

func Run() error {
	config := NewConfigFromFlags()

	if config.PortainerAddress == "" {
		return fmt.Errorf("portainer-address must be set")
	}

	if config.APIKey == "" {
		return fmt.Errorf("api-key must be set")
	}

	c := client.NewHTTPClient()
	c.Timeout = PortainerHTTPTimeout

	listR, err := http.NewRequest("GET", config.EndpointsURL(), nil)
	if err != nil {
		return fmt.Errorf("failed to create new GET request for endpoints: %w", err)
	}

	var endpoints Endpoints
	err = doRequest(&config, c, listR, &endpoints)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	endpointsByName := make(map[string]Endpoint)
	for _, e := range endpoints {
		endpointsByName[e.Name] = e
	}

	listS, err := http.NewRequest("GET", config.StacksURL(), nil)
	if err != nil {
		return fmt.Errorf("failed to create new GET request for stacks: %w", err)
	}

	var stackListResp StacksResponse
	err = doRequest(&config, c, listS, &stackListResp)
	if err != nil {
		return fmt.Errorf("failed to list stacks: %w", err)
	}

	var stacksById = make(map[string]Stack)
	for _, s := range stackListResp {
		stacksById[idFromStack(s)] = s
	}

	nodes, err := os.ReadDir(config.StackFilesDir)
	if err != nil {
		return fmt.Errorf("failed to read directory '%s': %w", config.StackFilesDir, err)
	}

	traverseNodes(config, nodes, endpointsByName, stacksById, c)

	if config.WatchStackdsDir {
		cc := make(chan notify.EventInfo, 1)

		// Set up a watchpoint listening for events within a directory tree rooted
		// at current working directory. Dispatch remove events to c.
		if err := notify.Watch(config.StackFilesDir+"...", cc, notify.All); err != nil {
			log.Fatal(err)
		}
		defer notify.Stop(cc)

		log.Printf("Watching: %s", config.StackFilesDir)

		for {
			// Block until an event is received.
			// log.Println("Waiting")
			ei := <-cc
			// log.Println("Got event:", ei)
			pathSplit := strings.Split(ei.Path(), "/")
			if len(pathSplit) < 3 {
				log.Printf("skip path: %s", ei.Path())
				continue
			}

			pathSplit = pathSplit[len(pathSplit)-3:]
			node := pathSplit[0]
			stack := pathSplit[1]

			log.Printf("deploy: %s/%s", node, stack)

			manageStack(&config, node, stack, endpointsByName, stacksById, c)
		}
	}

	return nil
}

func traverseNodes(config Config, nodes []os.DirEntry, ers map[string]Endpoint, sMap map[string]Stack, c *client.HTTPClient) {
	var wg sync.WaitGroup
	var done, count int

	for _, node := range nodes {
		// Each Node
		wg.Add(1)
		count++
		go func(node string) {
			defer wg.Done()
			dirs, err := os.ReadDir(config.StackFilesDir + node)
			if err != nil {
				log.Printf("failed to read directory './stacks/%s': %s", node, err)
				return
			}

			for _, stack := range dirs {
				wg.Add(1)
				count++
				go func(node, stack string) {
					defer wg.Done()
					manageStack(&config, node, stack, ers, sMap, c)
					done++
				}(node, stack.Name())
			}
			done++
		}(node.Name())
	}

	log.Printf("waiting for all stacks to be deployed")
	wgDone := false
	go func() {
		for {
			if wgDone {
				return
			}
			log.Printf("status: %d/%d", done, count)
			time.Sleep(5 * time.Second)
		}
	}()
	now := time.Now()
	wg.Wait()
	wgDone = true
	log.Printf("all stacks deployed in: %s", time.Since(now))
}

func manageStack(config *Config, node string, stack string, ers map[string]Endpoint, sMap map[string]Stack, c *client.HTTPClient) {
	if v1, ok := ers[node]; ok {
		stackID := fmt.Sprintf("%d-%s", v1.ID, stack)
		if _, ok := sMap[stackID]; ok {
			log.Printf("update stack %s on node %s", stack, node)
			err := updateStack(config, node, stack, v1, sMap, c)
			if err != nil {
				log.Printf("failed to update stack: %s", err)
			}
		} else {
			log.Printf("create stack %s on node %s", stack, node)
			err := createStack(config, node, stack, v1, c)
			if err != nil {
				log.Printf("failed to create stack: %s", err)
			}
			sMap[stackID] = Stack{}
		}
	}
}

func updateStack(config *Config, node string, stack string, endpoint Endpoint, sMap map[string]Stack, c *client.HTTPClient) error {
	sf, err := os.ReadFile(fmt.Sprintf("./stacks/%s/%s/docker-compose.yml", node, stack))
	if err != nil {
		return fmt.Errorf("failed to read file './stacks/%s/%s/docker-compose.yml': %w", node, stack, err)
	}

	s, ok := sMap[fmt.Sprintf("%d-%s", endpoint.ID, stack)]
	if !ok {
		return fmt.Errorf("stack %s not found on node %s", stack, node)
	}

	// fetch current stack and compare stackFile contents
	cr, err := http.NewRequest("GET", fmt.Sprintf("http://portainer.b.brain/api/stacks/%d/file", s.ID), nil)
	if err != nil {
		return fmt.Errorf("failed to create PUT request for stack update: %w", err)
	}

	var sfc CreateStackRequest
	err = doRequest(config, c, cr, &sfc)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	if strings.Compare(sfc.StackFileContent, string(sf)) == 0 {
		log.Printf("Skip identical: %s", stack)
		return nil
	}

	bodys, err := json.Marshal(BodyComposeUpdate{
		StackFileContent: string(sf),
		PullImage:        true,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal BodyComposeUpdate: %w", err)
	}
	body := bytes.NewBufferString(string(bodys))

	cr, err = http.NewRequest("PUT", fmt.Sprintf("http://portainer.b.brain/api/stacks/%d?endpointId=%d", s.ID, endpoint.ID), body)
	if err != nil {
		return fmt.Errorf("failed to create PUT request for stack update: %w", err)
	}

	err = doRequest(config, c, cr, nil)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	return nil
}

func createStack(config *Config, node string, stack string, endpoint Endpoint, c *client.HTTPClient) error {
	sf, err := os.ReadFile(fmt.Sprintf("./stacks/%s/%s/docker-compose.yml", node, stack))
	if err != nil {
		return fmt.Errorf("failed to read file './stacks/%s/%s/docker-compose.yml': %w", node, stack, err)
	}

	req := CreateStackRequest{
		Name:             stack,
		StackFileContent: string(sf),
	}

	s, err := json.Marshal(&req)
	if err != nil {
		return fmt.Errorf("failed to marshal CreateStackRequest: %w", err)
	}

	body := bytes.NewBufferString(string(s))
	cr, err := http.NewRequest("POST", fmt.Sprintf("http://portainer.b.brain/api/stacks?type=%d&method=%s&endpointId=%d", 2, "string", endpoint.ID), body)
	if err != nil {
		return fmt.Errorf("failed to create POST request for stack creation: %w", err)
	}

	err = doRequest(config, c, cr, nil)
	if err != nil {
		return fmt.Errorf("failed to create stack: %w", err)
	}

	return nil
}

func doRequest(config *Config, c *client.HTTPClient, req *http.Request, res interface{}) error {
	req.Header.Add("X-API-Key", config.APIKey)
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make POST request: %w", err)
	}
	defer resp.Body.Close()

	if res != nil {
		err = json.NewDecoder(resp.Body).Decode(res)
		if err != nil {
			return fmt.Errorf("could not decode response: %W", err)
		}
	}

	return nil
}

func idFromStack(s Stack) string {
	return fmt.Sprintf("%d-%s", s.EndpointId, s.Name)
}

type UpdateStackRequest struct {
	ID         int `json:"id"`
	EndpointID int `json:"endpointId"`
}

type CreateStackRequest struct {
	Name             string `json:"Name"`
	StackFileContent string `json:"StackFileContent"`
}

type BodyCompose struct {
	Name             string `json:"Name"`
	StackFileContent string `json:"StackFileContent"`
}

type BodyComposeUpdate struct {
	StackFileContent string `json:"stackFileContent"`
	PullImage        bool   `json:"pullImage"`
}
