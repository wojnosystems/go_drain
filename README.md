# Overview

go_drain is an abstraction and concretion for creating re-loadable configuration structures and issuing that configuration to go-routines via Claim. Once the go routine completes its request, it should call Release on the claim.

When it's time to reload the config, call ReOpen. If the configuration loads successfully, it will be swapped out for future calls to Claim. Any go-routines currently running are able to use existing claims until all are released.

# Copyright

Copyright © 2019 Chris Wojno. All rights reserved.

No Warranties. Use this software at your own risk.

# License

Attribution 4.0 International https://creativecommons.org/licenses/by/4.0/