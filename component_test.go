package go_drain

import (
	"fmt"
	"testing"
)

type omniConfig struct {
	dbComp        string
	serverComp    string
	invariantComp string

	dbConfig        string
	serverConfig    string
	invariantConfig string
}

func TestNewAuto(t *testing.T) {
	copyFromConfig := omniConfig{
		dbConfig:        "og",
		serverConfig:    "og",
		invariantConfig: "og",
	}

	closeDidRun := 0

	d, err := NewDrainWithComponents(func() (interface{}, error) {
		x := copyFromConfig
		return &x, nil
	}, []ComponentReloader{
		// DATABASE
		NewAutoComponent(func(buildingConfig interface{}) error {
			buildingConfig.(*omniConfig).dbComp = fmt.Sprintf(`running-db-%s`, buildingConfig.(*omniConfig).dbConfig)
			return nil
		}, func(buildingConfig interface{}) {
			closeDidRun++
			// Pretend to close by setting string to closed
			buildingConfig.(*omniConfig).dbComp = `closed`
		}, func(buildingConfig interface{}, currentlyRunningConfig interface{}) bool {
			// OK to copy if identical
			return buildingConfig.(*omniConfig).dbConfig == currentlyRunningConfig.(*omniConfig).dbConfig
		}, func(dst interface{}, src interface{}) {
			dst.(*omniConfig).dbComp = src.(*omniConfig).dbComp
		}),
		// SERVER
		NewAutoComponent(func(buildingConfig interface{}) error {
			buildingConfig.(*omniConfig).serverComp = fmt.Sprintf(`running-server-%s`, buildingConfig.(*omniConfig).serverConfig)
			return nil
		}, func(buildingConfig interface{}) {
			closeDidRun++
			// Pretend to close by setting string to closed
			buildingConfig.(*omniConfig).serverComp = `closed`
		}, func(buildingConfig interface{}, currentlyRunningConfig interface{}) bool {
			// OK to copy if identical
			return buildingConfig.(*omniConfig).serverConfig == currentlyRunningConfig.(*omniConfig).serverConfig
		}, func(dst interface{}, src interface{}) {
			dst.(*omniConfig).serverComp = src.(*omniConfig).serverComp
		}),
		// SOMETHING ELSE THAT DOESN'T CHANGE
		NewAutoComponent(func(buildingConfig interface{}) error {
			buildingConfig.(*omniConfig).invariantComp = fmt.Sprintf(`running-invariant-%s`, buildingConfig.(*omniConfig).invariantConfig)
			return nil
		}, func(buildingConfig interface{}) {
			closeDidRun++
			// Pretend to close by setting string to closed
			buildingConfig.(*omniConfig).invariantComp = `closed`
		}, func(buildingConfig interface{}, currentlyRunningConfig interface{}) bool {
			return false // never say it changed, always create a new one
		}, func(dst interface{}, src interface{}) {
			dst.(*omniConfig).invariantComp = src.(*omniConfig).invariantComp
		}),
	})

	if err != nil {
		t.Errorf(`got error, but did not expect it: %v`, err)
	}

	// expect
	if cc, err := d.Claim(); err == nil {
		cfg := cc.Config().(*omniConfig)
		if cfg.dbComp != `running-db-og` {
			t.Error(`dbComp configuration was not created`)
		}
		if cfg.serverComp != `running-server-og` {
			t.Error(`serverComp configuration was not created`)
		}
		if cfg.invariantComp != `running-invariant-og` {
			t.Error(`invariantComp configuration was not created`)
		}
		d.Release(&cc)
	}

	copyFromConfig.serverConfig = "upd"
	copyFromConfig.invariantConfig = "upd"

	// this should increment everything by 1 except for invariant, which should be copied
	_ = d.ReLoad()

	if closeDidRun != 2 {
		t.Error(`expected close methods to be called `, 2, ` times but was `, closeDidRun)
	}

	if cc, err := d.Claim(); err == nil {
		cfg := cc.Config().(*omniConfig)
		if cfg.dbComp != `running-db-og` {
			t.Error(`dbComp configuration was updated`)
		}
		if cfg.serverComp != `running-server-upd` {
			t.Error(`serverComp configuration was not updated`)
		}
		if cfg.invariantComp != `running-invariant-upd` {
			t.Error(`invariantComp configuration was updated`)
		}
		d.Release(&cc)
	}

	d.StopAndJoin()

	if closeDidRun != 5 {
		t.Error(`expected close methods to be called `, 5, ` times but was `, closeDidRun)
	}
}
