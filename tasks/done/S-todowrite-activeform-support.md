# Add activeForm support for TodoWrite

## Summary
Enhanced TodoWrite functionality to support activeForm field for in-progress task display, making TODO decoder more flexible with optional fields.

## Changes
- Made `id` and `priority` fields optional in Todo decoder
- Added activeForm support for displaying tasks with in-progress status
- Fixed command termination with proper cmd.Wait() call
- Exported encodeTodo and todosDecoder functions from Main module for testing
- Added comprehensive tests for JSON interop

## Status
Completed