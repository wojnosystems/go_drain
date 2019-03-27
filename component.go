package go_drainer

// ComponentOpenTestFunc creates the object from the configuration
// @param buildingConfig is the configuration to use when creating
//   this configuration. This will always be non-nil
// @return nil if no errors, the error encountered if there was a
//   problem creating the component
type ComponentOpenTestFunc func(buildingConfig interface{}) error

// ComponentCloseFunc shuts down the component, regardless of
//   re-use. Just close it
// @param buildingConfig is the configuration to use when creating
//   this configuration. This will always be non-nil
type ComponentCloseFunc func(buildingConfig interface{})

// ComponentShouldCopyFunc true to re-use the component, false to
//   close and create a new one
// @param buildingConfig is the configuration to use when creating
//   this configuration. This will always be non-nil
// @param currentlyRunningConfig is the configuration to compare
//   the current configuration with to determine if it changed.
//   This will always be non-nil
// @return true if the configuration for the component in question
//   is the same different and the component should be copied, false if
//   they are different and the component should be re-created
type ComponentShouldCopyFunc func(buildingConfig interface{}, currentlyRunningConfig interface{}) bool

// ComponentCopyFunc moves the configured value (hopefully a pointer)
//   from the running configuration into the new configuration. This
//   allows you to re-use components if they have not changed
// @param dst is where the value is copied into. This will always be non-nil
// @param src is where the value is coped from. This will always be non-nil
type ComponentCopyFunc func(dst interface{}, src interface{})

// ComponentReloader is the generic interface used to control how
// items are to be loaded, unloaded, tested, swapped, and whether
// they should be swapped
type ComponentReloader interface {
	// OpenAndTest given a config, create a new component with that
	// configuration. Test it and return any errors building or testing
	OpenAndTest(buildingConfig interface{}) error

	// Close given a config, close down the resources associated with this component
	// don't worry about re-using or copying. This is handled for you, just provide the logic to close
	Close(buildingConfig interface{})

	// ShouldCopy compare the new and currentlyRunningConfig and if the old config value
	// should be used, return true. To close the old one and create a new one, return false
	ShouldCopy(buildingConfig interface{}, currentlyRunningConfig interface{}) bool

	// Copy move the component from src to dst.
	Copy(dst interface{}, src interface{})
}

// baseComponent concretion used in NewDrainWithComponents
type baseComponent struct {
	openAndTestFunc ComponentOpenTestFunc
	closeFunc       ComponentCloseFunc
	shouldCopyFunc  ComponentShouldCopyFunc
	copyFunc        ComponentCopyFunc
}

// NewDrainWithComponents builds a Drainer object that knows how to build/reload a
// configuration object (called on reload and on creation) and will build and test
// the items in buildOrder and close them in REVERSE order. This also has the logic
// to perform component copying when re-using components that don't change
// @param configBuilder is a factory that builds new configuration objects. This
// object should also have the data required to bootstrap components as well as
// store those components
// @param buildOrder is an array of ComponentReloader objects that build a single
// component in the configuration at a time, such as logging, then database, then
// cache servers, then http servers, and so on
// @return Drainer object, ready for work or nil if error
// @return error if there was an error building any of the components the first time, nil if no errors
func NewDrainWithComponents(configBuilder func() interface{}, buildOrder []ComponentReloader) (Drainer, error) {
	return New(func(currentlyRunningConfig interface{}) (newConfig interface{}, err error) {
		cfg := configBuilder()
		for i := range buildOrder {
			// if already created and not changed, use that old configuration
			if currentlyRunningConfig != nil && !buildOrder[i].ShouldCopy(cfg, currentlyRunningConfig) {
				buildOrder[i].Copy(cfg, currentlyRunningConfig)
			} else {
				// if nothing running, or changed, create a new item
				err = buildOrder[i].OpenAndTest(cfg)
				if err != nil {
					// error encountered when creating or testing this component
					return
				}
			}
		}
		return cfg, nil
	}, func(configToClose interface{}, currentlyRunningConfig interface{}) {
		for i := len(buildOrder) - 1; i >= 0; i-- {
			// no config is currently running, always close OR the config has changed, OK to close it
			if currentlyRunningConfig == nil || buildOrder[i].ShouldCopy(configToClose, currentlyRunningConfig) {
				buildOrder[i].Close(configToClose)
			}
		}
	})
}

// NewAutoComponent creates a new component factory that allows the component-drain to build configs without much intervention on your behalf
// @param openAndTestFunc is a function that builds a component, without regard if it needs to be Copied or closed first. Leave that to the AutoDrain
// @param closeFunc is a function that shuts-down and/or releases the resources for the component
// @param shouldCopyFunc is a function that indicates with true if the component should be re-used instead of closing and opening it again. If nil, will act as though you used a function that always returns false. This method is not called if copyFunc is nil.
// @param copyFunc is a function that copies the configuration from the currently running configuration to the new configuration, in lieu of closing and re-opening it. Pass in nil to never copy and always create new items
func NewAutoComponent(
	openAndTestFunc ComponentOpenTestFunc,
	closeFunc ComponentCloseFunc,
	shouldCopyFunc ComponentShouldCopyFunc,
	copyFunc ComponentCopyFunc) ComponentReloader {
	return &baseComponent{
		openAndTestFunc: openAndTestFunc,
		closeFunc:       closeFunc,
		shouldCopyFunc:  shouldCopyFunc,
		copyFunc:        copyFunc,
	}
}

// OpenAndTest is a pass-through to the function in the object
func (a *baseComponent) OpenAndTest(buildingConfig interface{}) error {
	return a.openAndTestFunc(buildingConfig)
}

// Close is a pass-through to the function in the object
func (a *baseComponent) Close(buildingConfig interface{}) {
	a.closeFunc(buildingConfig)
}

// ShouldCopy is a pass through unless the function is nil or if the CopyFunc is nil. If either are nil, then
// @return false
func (a *baseComponent) ShouldCopy(buildingConfig interface{}, currentlyRunningConfig interface{}) bool {
	if a.shouldCopyFunc != nil && a.copyFunc != nil {
		return a.shouldCopyFunc(buildingConfig, currentlyRunningConfig)
	}
	return false
}

// Copy is a pass through unless the CopyFunc is nil. If nil, then is a no-op
func (a *baseComponent) Copy(dst interface{}, src interface{}) {
	if a.copyFunc != nil {
		a.copyFunc(dst, src)
	}
}
