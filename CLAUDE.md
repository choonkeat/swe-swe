- ignore the content of ./external
- always use `make` to run test or build the programs, e.g. `make test` `make build`
- do NOT start swe-swe process yourself. use `make local-restart` and a new build will start if it builds

## IMPORTANT: Permission Handling
- When you get permission errors like "Claude requested permissions to [tool]" or "This command requires approval", DO NOT attempt to work around them
- DO NOT suggest alternative approaches or try different methods when permissions are required
- Simply STOP and wait for the user to grant permission through the permission dialog
- DO NOT explain what you would do if permission was granted - just wait
- The system will automatically retry your exact same command once permission is granted

