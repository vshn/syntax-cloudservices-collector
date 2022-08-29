package main

import (
	"fmt"
	"os"
	"strings"
)

type K8sConfig struct {
	Name  string
	Api   string
	Token string
}

func cfg() (string, string, map[string]*K8sConfig) {
	exoscaleApiKey := os.Getenv(keyEnvVariable)
	if exoscaleApiKey == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Environment variable %s must be set\n", keyEnvVariable)
		os.Exit(1)
	}

	exoscaleApiSecret := os.Getenv(secretEnvVariable)
	if exoscaleApiSecret == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Environment variable %s must be set\n", secretEnvVariable)
		os.Exit(1)
	}

	k8sConfigs := make(map[string]*K8sConfig)
	for _, e := range os.Environ() {
		varName := strings.Split(e, "=")[0]
		if strings.HasPrefix(varName, k8sApiPrefix) && len(varName) > len(k8sApiPrefix) {
			name := strings.ToLower(varName[len(k8sApiPrefix):])
			if _, ok := k8sConfigs[name]; !ok {
				k8sConfigs[name] = &K8sConfig{Name: name}
			}
			k8sConfig := k8sConfigs[name]
			k8sConfig.Api = os.Getenv(varName)
			continue
		}
		if strings.HasPrefix(varName, k8sTokenPrefix) && len(varName) > len(k8sTokenPrefix) {
			name := strings.ToLower(varName[len(k8sTokenPrefix):])
			if _, ok := k8sConfigs[name]; !ok {
				k8sConfigs[name] = &K8sConfig{Name: name}
			}
			k8sConfig := k8sConfigs[name]
			k8sConfig.Token = os.Getenv(varName)
			continue
		}
	}
	// remove incomplete K8sConfigs from map
	for name, k8sConfig := range k8sConfigs {
		if k8sConfig.Token == "" {
			fmt.Fprintf(os.Stderr, "K8s %s is missing token, ignoring\n", k8sConfig.Name)
			delete(k8sConfigs, name)
		} else if k8sConfig.Api == "" {
			fmt.Fprintf(os.Stderr, "K8s %s is missing api, ignoring\n", k8sConfig.Name)
			delete(k8sConfigs, name)
		} else {
			fmt.Fprintf(os.Stderr, "K8s %s configuration OK\n", name)
		}
	}

	return exoscaleApiKey, exoscaleApiSecret, k8sConfigs
}
