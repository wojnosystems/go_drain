package go_drainer

import (
	"container/list"
	"errors"
	"sync"
)

// Drain is a way to create configurations and rotate them out whenever needed.
// The triggering mechanism to rotate a configuration is calling ReLoad. When
// creating a Drainer object, you pass in the loadAndTester function, which you
// define to create and then verify your own custom configuration. Configurations
// can be database connections or loading from files or what-have you. If you
// return an error, then the configuration will not be swapped in and routines
// calling Claim will continue to get the working configuration. However, if you
// do not return an error, the configuration will be swapped in and routines
// calling Claim will get the new configuration state/objects.
//
// The closer method cleans up the configuration. It should be written to ignore
// nil configurations, which is certain to be encountered on the first call to
// loadAndTester.

// LoadAndTesterType is a function called to load the configuration and test it
// If any errors are returned, this configuration will not be swapped in. If an
// error is returned, CloserFunc is called to clean up after the configuration,
// so be sure your configuration can handle uninitialized values
// @param currentConfig is the most recent configuration. If this is the first
//   run, this will be nil. This is useful if swapping out sockets or doing
//   other things that require a shutdown and restart of some configuration-
//   dependent structure. Passing in the current configuration allows you
//   the ability to compare the current configuration with the new configuration
//   so if a socket hasn't changed, you don't need to create a new http listener.
//   Just be sure you don't close that listener on yourself ;)
// @return config your configuration object. This will be returned to callers of "Claim"
// @return err is any error encountered when loading the configuration
type LoadAndTesterFunc func(currentConfig interface{}) (config interface{}, err error)

// CloserType is the function called to shutdown or release the
// resources used by the configuration
// @param config is the configuration object created by LoaderType
type CloserFunc func(config interface{})

// ConfigClaim holds the configuration claim
// The version is used to determine which version
// of the config to clean up
type ConfigClaim struct {
	// version is the version of the configuration this structure points to
	version uint64

	// config is an interface to allow users to submit any configuration
	config interface{}
}

// Version gets the version of the configuration
func (c ConfigClaim) Version() uint64 {
	return c.version
}

// Config gets a pointer to the configuration
// Callers can cast this return type to the type returned from loadAndTester
func (c ConfigClaim) Config() interface{} {
	return c.config
}

// Invalidate resets the claim to prevent misuse
func (c *ConfigClaim) Invalidate() {
	c.version = 0
	c.config = nil
}

// Drainer is an interface that defines methods
// to enable configurations to be rotated
type Drainer interface {
	// Claim gets a pointer to the current configuration and the
	// current version. This begins the process of tracking that
	// some go routine has a copy of the configuration If you
	// make a call to Claim, you MUST call Release to ensure
	// data is cleaned up
	Claim() ConfigClaim

	// Release indicates that the go routine is finished with
	// the configuration when all claims are returned, the
	// closer method will be called if there's a new
	// configuration, or the Drain is stopped
	// When released, the ConfigClaim is zero'ed out
	Release(*ConfigClaim)

	// ReLoad triggers re-loading of the configuration. If there's
	// an error, the new config is discarded and the swap is not
	// performed. If the reload succeeds, the new config is made
	// the current version and new calls to Claim get the new
	// configuration.
	ReLoad() error

	// Stop triggers calls to Claim to fail
	// Stop does not wait for routines to complete and returns immediately (won't block)
	// Stop, if called while no claims are Claimed, will clean up the configuration immediately
	// If Claims are outstanding, the config will be cleaned up when all Claims are Released
	Stop()

	// StopAndJoin prevents Claim calls from working and will trigger a
	// shutdown of the configuration. StopAndJoin will block until all routines
	// have Released their Claims.
	StopAndJoin()
}

// configVersion is the pair that holds the config and the count
// of that config
type configVersion struct {
	// count is how many go routines currently are using this
	// copy of the configuration
	count uint64

	// version is which configuration this represents
	version uint64

	// config is the actual configuration data
	config interface{}
}

// ErrDrainAlreadyStopped is returned when Claim is called on a closed Drain
var ErrDrainAlreadyStopped = errors.New(`drain already stopped`)

// Drain contains the life-cycle state
type Drain struct {
	// mu is used to ensure that data is synchronized between routines
	mu sync.Mutex

	// closeWg counts how many copies of all configurations are outstanding
	// once all of those configurations are released, StopAndJoinError will
	// return
	closeWg sync.WaitGroup

	// versionTracking tracks how many of the configuration version are outstanding in go routines
	// the latest configuration is at the back, the oldest are at the front.
	// versionTracking contains type: *configVersion
	versionTracking *list.List

	// loader is the method that is called to load & test the configuration
	loadAndTester LoadAndTesterFunc

	// closer is the method that is called to shutdown or close resources used by the configuration
	closer CloserFunc

	// isStopped tracks if the Drain is stopped
	isStopped bool
}

// NewDrain creates a Drain object
//
// If loadAndTester returns an error the first time, it will be returned on this
// call and the returned drain will be nil
//
// @param loadAndTester is the function the creates a new configuration. It is also
//   the function that tests that configuration. If an error is returned, the
//   configuration will not be swapped out
// @param closer is the function that shuts down and releases resources in the
//   configuration. In the event loadAndTester returns an error, the returned
//   configuration, if any, will be returned to this method upon failure to
//   allow you a single place to clean up the configuration.
// @return c the Drain object or nil, if there was an error
// @return err any errors encountered when loading or testing the config
func New(
	loadAndTest LoadAndTesterFunc,
	closer CloserFunc,
) (c *Drain, err error) {
	c = &Drain{
		versionTracking: list.New(),
		loadAndTester:   loadAndTest,
		closer:          closer,
	}
	// perform the initial load
	cv, err := c.doLoadAndTest()
	if err != nil {
		return nil, err
	}

	// first version starts at 1
	// that way, object with version 0 are invalid
	cv.version = 1

	// Set the config
	c.versionTracking.PushBack(&cv)

	// by this point, everything is loaded and ready
	return c, nil
}

// Claim is a routine-safe way of obtaining the configuration
// @return cc the configuration with version number embedded for
//  future release or an invalidated claim if Drain is already closed
// @return err ErrDrainAlreadyStopped if StopAndJoin has been called, nil otherwise
func (c *Drain) Claim() (cc ConfigClaim, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isStopped {
		return ConfigClaim{}, ErrDrainAlreadyStopped
	}
	cc = ConfigClaim{}
	e := c.versionTracking.Back()
	if e == nil {
		// No versions configured, return a nil version
		return cc, nil
	}
	// Don't track this as outstanding until a real version is established
	c.closeWg.Add(1)
	ccv := e.Value.(*configVersion)
	ccv.count++

	cc.version = ccv.version
	cc.config = ccv.config
	return cc, nil
}

// Release counts the ConfigClaim when performing draining.
// @param cc is the configuration claim provided by calling "Claim".
//   you must call Release as it indicates to the Drain that
//   you're completed using the configuration. When Release returns,
//   the ConfigClaim is Invalidated, meaning calling Config() will return nil
//   this is to provide safety to avoid using resources later that may no longer
//   be open or configured. You must never use a configuration contained within
//   the ConfigClaim after calling Release on it, otherwise, those resources
//   that it references may be closed or shutdown
func (c *Drain) Release(cc *ConfigClaim) {
	if cc == nil || cc.version == 0 {
		// no version, just discard
		return
	}
	c.mu.Lock()
	e := c.findElementWithVersion(cc.version)
	if e == nil {
		// no record found, just return, nothing to do
		// this can happen if Claim was called and threw an error,
		// but they released the version anyway
		return
	}
	ccv := e.Value.(*configVersion)
	ccv.count--
	c.closeWg.Done()
	// only drain if not the current count and the outstanding count is zero
	// we do not want to clean up if we have no active threads as a new one may appear
	if c.shouldCleanup(*ccv) {
		// cleanup this config
		c.versionTracking.Remove(e)

		// unlock before allowing config to get cleaned up, as that could be along time
		c.mu.Unlock()

		// perform cleanup
		c.closer(cc.config)

		// call Invalidate before returning to prevent using old configuration data
		cc.Invalidate()
	} else {
		// be sure to unlock before returning
		c.mu.Unlock()
	}
	return
}

// shouldCleanup is true if this configuration should be closed/cleaned up
// This occurs when all go routines have released their claims for a version
// UNLESS it's the latest version. If the StopAndJoinError has been called,
// all configurations will be closed, even if the configuration is the
// latest version. This way, if the system is still running, the last
// configuration will not be closed, but if stopped, it will be cleaned up
// when all routines have released their claims.
// @param cv is the configuration version to check
// @return true if cleanup should happen, false if not
func (c *Drain) shouldCleanup(cv configVersion) bool {
	return cv.count == 0 &&
		(c.isStopped || c.versionTracking.Back().Value.(*configVersion).version != cv.version)
}

// findElementWithVersion takes the version and returns the element with that version
// @return the element with the version or nil, if not found
func (c *Drain) findElementWithVersion(version uint64) (e *list.Element) {
	for e = c.versionTracking.Front(); e != nil; e = e.Next() {
		if e.Value.(*configVersion).version == version {
			return e
		}
	}
	return nil
}

// doLoadAndTest calls loader and tester, returning any errors encountered.
// If an error is returned, closer is called on the config returned by loadAndTester
// This allows the user to clean up a partially configured config.
// @return cv is the configVersion with the configuration. It does NOT have the version field populated.
// @return err the error returned by loader and tester, or nil if any
func (c *Drain) doLoadAndTest() (cv configVersion, err error) {
	// perform the initial load
	cfg, err := c.Claim()

	// Perform the load
	cv.config, err = c.loadAndTester(cfg.config)

	// Ensure that the configuration is released
	c.Release(&cfg)

	// LoadAndTester threw an error, close down the broken/partially working configuration
	if err != nil {
		c.closer(cv.config)
		return
	}
	return
}

// ReLoad triggers the loader and tester to fire (without a lock). If there
// are no errors, that configuration will be atomically appended to the Drain
// as the latest version and will be returned in future calls to Claim. Once
// all calls to Release are made, that version of the configuration will be
// closed using the closer function.
// @return err the error encountered during loader and tester
func (c *Drain) ReLoad() (err error) {
	// perform the initial load
	var cv configVersion
	cv, err = c.doLoadAndTest()
	if err != nil {
		// if there is an error, do NOT change the state of the Drain
		return
	}

	// Set the config
	c.mu.Lock()
	defer c.mu.Unlock()
	// append the new version to the back of the list, making it the latest version
	// there will always be at least 1 version
	ccv := c.versionTracking.Back().Value.(*configVersion)
	cv.version = ccv.version + 1
	c.versionTracking.PushBack(&cv)
	return
}

// Stop prevents Claim calls from returning actual values
// It's possible to call Stop and no Claims are outstanding
// in this case, we'll clean up the last version
func (c *Drain) Stop() {
	c.mu.Lock()
	c.isStopped = true
	// it's possible that all threads were done but were not
	// cleaned up as the StopAndJoin method was called after all routines
	// have ceased requesting Claims, in this case, we need to clean up
	e := c.versionTracking.Back()
	if e != nil {
		c.versionTracking.Remove(e)
		c.mu.Unlock()
		// unlock while calling closer, could be long
		c.closer(e.Value.(*configVersion).config)
	} else {
		c.mu.Unlock()
	}
}

// StopAndJoin prevents new calls to Claim from returning valid results
// StopAndJoin will wait for outstanding routines that have Claims to call Release on those claims
func (c *Drain) StopAndJoin() {
	// set the state, need to lock to do this
	// unlock to allow claims to be released
	c.Stop()

	// wait for everything to be released
	c.closeWg.Wait()

	// No threads should be operating at this point
	c.mu.Lock()
	// it's possible that all threads were done but were not
	// cleaned up as the StopAndJoin method was called after all routines
	// have ceased requesting Claims, in this case, we need to clean up
	e := c.versionTracking.Back()
	if e != nil {
		c.versionTracking.Remove(e)
		c.mu.Unlock()
		// unlock while calling closer, could be long
		c.closer(e.Value.(*configVersion).config)
	} else {
		c.mu.Unlock()
	}
}

func (c *Drain) cleanLastVersion() {
}
