package main

import (
	"testing"

	"github.com/flowercomputers/flop/api"
)

func TestDeploymentColumns(t *testing.T) {
	pending := api.Deployment{Generation: 1, DesiredState: api.DesiredRunning}
	rolling := api.Deployment{Generation: 2, DesiredState: api.DesiredRunning,
		Observed: &api.Observation{Generation: 1, State: api.BogStateRunning, Healthy: true}}
	quiet := api.Deployment{Generation: 1, DesiredState: api.DesiredRunning,
		Observed: &api.Observation{Generation: 1, State: api.BogStateUnknown}}

	cases := []struct{ got, want string }{
		{deploymentState(pending), "pending"},
		{deploymentHealth(pending), "-"},
		{deploymentGen(rolling), "1→2"},
		{deploymentState(rolling), "running"},
		{deploymentHealth(rolling), "healthy"},
		{deploymentGen(quiet), "1"},
		{deploymentHealth(quiet), "-"},
		{imageTag("123456789012.dkr.ecr.us-east-2.amazonaws.com/bogs/flower/toybog:v1"), "toybog:v1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
