package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"
	"k8s.io/kubernetes/pkg/client/restclient"
)

type Config struct {
	Clusters []struct {
		Name    string
		Cluster Cluster
	}
	Contexts []struct {
		Name    string
		Context Context
	}
	CurrentContext string `yaml:"current-context"`
	Users          []struct {
		Name string
		User User
	}
}

type Cluster struct {
	InsecureSkipTlsVerify bool `yaml:"insecure-skip-tls-verify"`
	Server                string
}

type Context struct {
	Cluster   string
	Namespace string
	User      string
}

type User struct {
	Username string
	Password string
	Token    string
}

func getConfig() (*restclient.Config, error) {
	home := os.Getenv("HOME")
	dat, err := ioutil.ReadFile(filepath.Join(home, ".kube/config"))
	if err != nil {
		return nil, err
	}

	cfg := &Config{}

	err = yaml.Unmarshal(dat, cfg)
	if err != nil {
		return nil, err
	}

	cluster, user, err := getCurrent(cfg)
	if err != nil {
		return nil, err
	}

	return &restclient.Config{
		Host:        cluster.Server,
		Username:    user.Username,
		Password:    user.Password,
		BearerToken: user.Token,
		Insecure:    cluster.InsecureSkipTlsVerify,
	}, nil
}

func getCurrent(cfg *Config) (*Cluster, *User, error) {
	var context *Context
	for _, ctx := range cfg.Contexts {
		if ctx.Name == cfg.CurrentContext {
			context = &ctx.Context
			break
		}
	}
	if context == nil {
		return nil, nil, fmt.Errorf("Context not found: %v", cfg.CurrentContext)
	}

	var cluster *Cluster
	for _, cls := range cfg.Clusters {
		if cls.Name == context.Cluster {
			cluster = &cls.Cluster
			break
		}
	}
	if cluster == nil {
		return nil, nil, fmt.Errorf("Cluster not found: %v", context.Cluster)
	}

	var user *User
	for _, usr := range cfg.Users {
		if usr.Name == context.User {
			user = &usr.User
			break
		}
	}
	if user == nil {
		return nil, nil, fmt.Errorf("User not found: %v", context.User)
	}

	return cluster, user, nil
}
