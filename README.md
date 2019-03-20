# Overview

go_drain is an abstraction and concretion for creating re-loadable configuration structures and issuing that configuration to go-routines via Claim. Once the go routine completes its request, it should call Release on the claim. This is intended to be used among many go-routines, so this structure is guarded against race conditions using mutexes and wait groups.

When it's time to reload the config, call ReLoad. If the configuration loads successfully, it will be swapped out for future calls to Claim. Any go-routines currently running are able to use existing claims until all are released.

# Usage

```go
package main

import (
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
    "github.com/wojnosystems/go_drain"
    "log"
    "net/http"
    "os"
)

type myConf struct {
	db *sql.DB
	/// other stuff
}

func main() {
	// create a new reloadableConfig
    reloadableConfig, err := go_drain.New( func(currentConfig interface{}) (config interface{}, err error) {
        // ignoring currentConfig for this example
        c := &myConf{}
        // reading db settings from ENV, obviously, don't really 
        // do this, this is just for demonstration purposes.
        // you can easily trigger a file read here to pull in settings from a file
        c.db, err = sql.Open("mysql", os.Getenv("DB_CONNECTION_STRING"))
        if err != nil {
            return nil, err
        }
        
        // BEGIN CONFIG TESTS!
        
        // check that database is alive!
        err = c.db.Ping()
        
        return c, err
    }, func(currentConfig interface{}) {
    	if currentConfig == nil {
    		// ensure configuration is valid
    		return
    	}
        c := currentConfig.(*myConf)
        if c.db != nil {
        	// if we configured a database object, close it to free up 
        	// its resources and close the connections used.
        	_ = c.db.Close()
        }
    } )
    if err != nil {
    	log.Fatal(`failed to initialize the settings!`)
    }
    
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        cfg, err := reloadableConfig.Claim()
        if err != nil {
        	// config is invalid, called Claim() after Stop()
        	// you should really shutdown your http server BEFORE you call Stop()
        	// this gives routines a chance to complete their work before returning
        	// control to the routine that called Stop
        	return
        }
        c := cfg.Config().(*myConf)
        
        // do stuff with configuration, use your imagination here.
        _, _ = c.db.Exec(`...DO THING...`)
        
        // when done, release
        reloadableConfig.Release(&cfg)
    })
    go func() {
        _ = http.ListenAndServe(":8080", nil )
    }()
    
    // time goes by...
    // some signal for SIGHUP comes in, time to reload the config!
    // this will open a new connection to the database after reading
    // the environment variable
    _ = reloadableConfig.ReLoad()
    
    // Wait to exit here say, on SIGINT or something
    
    // Stop will wait until all configurations are Released. New calls to Claim return an error and no valid configuration
    reloadableConfig.StopAndJoin()
}
```

The above is obviously a contrived and curtailed answer, but the program initializes a reloadableConfig by creating a new Drain. New takes two parameters, a way to load and test the configuration, and a way to clean up after the configuration.

In this example, we load the database connection settings from an environment variable. When the ```reloadableConfig.ReLoad()``` is called, it will re-read the environment variable, which may have been updated. The new value is read and the new database connection is established.

Any threads using the old configuration are allowed to complete. Once they ALL have called Release, that database connection will be closed.

Any new calls to Claim will return the NEW version of the database connection, while old connections will still use the old database connection. The rug will not be pulled out from under any process. Http requests will be served uninterrupted.

Finally, calling Stop will block the calling thread until all configuration objects have been closed.

# Copyright

Copyright © 2019 Chris Wojno. All rights reserved.

No Warranties. Use this software at your own risk.

# License

Attribution 4.0 International https://creativecommons.org/licenses/by/4.0/