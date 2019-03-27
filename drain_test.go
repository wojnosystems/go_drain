package go_drainer

import (
	"testing"
)

type myConfig struct {
	name string
}

func TestConfigLifeCycle_Claim(t *testing.T) {
	closeCalled := false
	loadCalled := 0

	cf, err := New(func(currentConfig interface{}) (config interface{}, err error) {
		cfg := &myConfig{}
		if loadCalled == 0 {
			cfg.name = "chris"
		} else {
			if currentConfig.(*myConfig).name != "chris" {
				t.Error(`expected currentConfig to have the old value`)
			}
			cfg.name = "wojno"
		}
		loadCalled++
		return cfg, nil
	}, func(configToClose interface{}, currentlyRunningConfig interface{}) {
		closeCalled = true
	})
	if err != nil {
		t.Fatal(err)
	}

	if loadCalled != 1 {
		t.Error("expected loadCalled to be 1, but got: ", loadCalled)
	}

	// Claim a config
	cfgV, err := cf.Claim()
	if err != nil {
		t.Error("claim without stop should not return an error, but got: ", err)
	}

	// Confirm that the config is working
	cfg := cfgV.Config().(*myConfig)
	if cfg.name != "chris" {
		t.Error(`expected name to be chris, but got: `, cfg.name)
	}

	// perform a reload
	err = cf.ReLoad()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure close not called until release is called
	if closeCalled {
		t.Error("expected the ReLoad to not to call the CloseFunc until Release is called")
	}

	// Release the claim
	cf.Release(&cfgV)

	// Ensure closeFunc is called
	if !closeCalled {
		t.Error("expected the ReLoad to call the CloseFunc, but it did not")
	}

	// reset the closeFunc was called checker
	closeCalled = false

	cfgV, err = cf.Claim()
	if err != nil {
		t.Error("claim without stop should not return an error, but got: ", err)
	}

	cfg = cfgV.Config().(*myConfig)
	if cfg.name != "wojno" {
		t.Error(`expected name to be wojno, but got: `, cfg.name)
	}

	// Release the claim
	cf.Release(&cfgV)

	// Ensure closeFunc is NOT called
	if closeCalled {
		t.Error("expected the Release not to call the CloseFunc")
	}

	cf.StopAndJoin()

	// Ensure closeFunc is called
	if !closeCalled {
		t.Error("expected the StopAndJoin to call the CloseFunc")
	}
}

func TestInterfaceImplementation(t *testing.T) {
	var drainer Drainer
	drainer = &Drain{}
	_ = drainer
}
