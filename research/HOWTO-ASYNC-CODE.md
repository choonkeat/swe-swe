# How async-code Integrates with Claude and Codex CLIs

This document provides a comprehensive technical analysis of how the `async-code` application integrates with both the `claude` and `codex` CLI tools to provide automated code generation through a web interface.

## Architecture Overview

The async-code system is a more complex, production-grade application compared to swe-swe:

1. **Backend (Python/Flask)**: `ObservedObserver/async-code/server/` - REST API with Docker-based task execution
2. **Frontend (Next.js/React)**: `ObservedObserver/async-code/async-code-web/` - Modern React UI with real-time polling
3. **Task Execution**: Docker containers running Claude or Codex CLI tools
4. **Data Storage**: Supabase for persistent task and user data

## Key Differences from swe-swe

1. **No WebSocket**: Uses HTTP polling instead of WebSocket for status updates
2. **Docker Isolation**: Each task runs in an isolated Docker container
3. **Multi-Model Support**: Supports both Claude and Codex (OpenAI) CLI tools
4. **Database Persistence**: Tasks are stored in Supabase, not in-memory
5. **GitHub Integration**: Direct PR creation via GitHub API
6. **User Management**: Full authentication and user preference storage

## Backend Implementation

### Core Components

1. **main.py**: Flask application entry point
2. **tasks.py**: Task management endpoints and PR creation logic
3. **utils/code_task_v2.py**: Docker container orchestration for CLI execution
4. **database.py**: Supabase integration for persistence
5. **models.py**: Data models and enums

### Task Execution Flow

1. **Task Creation** (`/start-task`):
```python
# tasks.py:15-75
@tasks_bp.route('/start-task', methods=['POST'])
def start_task():
    # Extract parameters including model selection
    model = data.get('model', 'claude')  # 'claude' or 'codex'
    
    # Create task in database
    task = DatabaseOperations.create_task(...)
    
    # Start task in background thread
    thread = threading.Thread(target=run_ai_code_task_v2, args=(task['id'], user_id, github_token))
    thread.daemon = True
    thread.start()
```

2. **Docker Container Execution**:
```python
# utils/code_task_v2.py:92-799
def _run_ai_code_task_v2_internal(task_id, user_id, github_token):
    # Container configuration based on model
    if model_cli == 'codex':
        container_image = 'codex-automation:latest'
    else:
        container_image = 'claude-code-automation:latest'
```

### Claude CLI Integration

The Claude integration is sophisticated, handling multiple execution scenarios:

```bash
# utils/code_task_v2.py:348-456
# Try different ways to invoke claude
if [ -f /usr/local/bin/claude ]; then
    # Check if it's a shell script
    if head -1 /usr/local/bin/claude | grep -q "#!/bin/sh"; then
        sh /usr/local/bin/claude < /tmp/prompt.txt
    # Check if it's a Node.js script
    elif head -1 /usr/local/bin/claude | grep -q "#!/usr/bin/env.*node"; then
        # Use --print flag for non-interactive mode
        cat /tmp/prompt.txt | node /usr/local/bin/claude --print --allowedTools "Edit,Bash"
    fi
fi
```

Key Claude flags:
- `--print`: Non-interactive mode (similar to swe-swe)
- `--allowedTools "Edit,Bash"`: Restricts available tools for safety

### Codex CLI Integration

Codex requires special handling due to its sandboxing features:

```bash
# utils/code_task_v2.py:286-346
# Set environment variables for non-interactive mode
export CODEX_QUIET_MODE=1
export CODEX_UNSAFE_ALLOW_NO_SANDBOX=1
export CODEX_DISABLE_SANDBOX=1
export CODEX_NO_SANDBOX=1

# Use official non-interactive flags
/usr/local/bin/codex --approval-mode full-auto --quiet "$PROMPT_TEXT"
```

Key Codex configurations:
- `--approval-mode full-auto`: Automatic approval without user interaction
- `--quiet`: Suppress interactive prompts
- Multiple sandbox disable flags to prevent Docker-in-Docker issues

### Security and Isolation

1. **Container Security** (for Codex):
```python
# utils/code_task_v2.py:549-561
if model_cli == 'codex':
    container_kwargs.update({
        'security_opt': [
            'seccomp=unconfined',      # Disable seccomp filtering
            'apparmor=unconfined',     # Disable AppArmor
            'no-new-privileges=false'  # Allow privilege escalation
        ],
        'cap_add': ['ALL'],            # Grant all capabilities
        'privileged': True,            # Full privileges
        'pid_mode': 'host'            # Share host PID namespace
    })
```

2. **Resource Limits**:
```python
'mem_limit': '2g',
'cpu_shares': 1024,
'ulimits': [docker.types.Ulimit(name='nofile', soft=1024, hard=2048)]
```

### Output Parsing

The system captures and parses structured output from the CLI tools:

```python
# utils/code_task_v2.py:670-760
# Parse output markers
if line == '=== PATCH START ===':
    capturing_patch = True
elif line == '=== GIT DIFF START ===':
    capturing_diff = True
elif line == '=== CHANGED FILES START ===':
    capturing_files = True
elif line == '=== FILE CHANGES START ===':
    capturing_file_changes = True
```

This allows extraction of:
- Git patches for later PR creation
- File diffs for UI display
- Before/after file content for merge views

### GitHub PR Creation

Unlike swe-swe, async-code creates PRs server-side:

```python
# tasks.py:340-469
@tasks_bp.route('/create-pr/<int:task_id>', methods=['POST'])
def create_pull_request(task_id):
    # Apply patch using GitHub API
    files_updated = apply_patch_to_github_repo(repo, pr_branch, patch_content, task)
    
    # Create pull request
    pr = repo.create_pull(
        title=pr_title,
        body=pr_body,
        head=pr_branch,
        base=base_branch
    )
```

The patch application uses GitHub's Tree API for atomic commits:
```python
# tasks.py:586-622
# Create tree elements for all changed files
for file_path, new_content in files_to_update.items():
    blob = repo.create_git_blob(new_content, "utf-8")
    tree_elements.append({
        "path": file_path,
        "mode": "100644",
        "type": "blob",
        "sha": blob.sha
    })

# Create a single commit with all changes
new_tree = repo.create_git_tree(tree_elements, base_tree=current_commit.commit.tree)
new_commit = repo.create_git_commit(message=commit_message, tree=new_tree, parents=[current_commit.commit])
```

## Frontend Implementation

### Technology Stack

- **Framework**: Next.js 14 with App Router
- **UI Components**: shadcn/ui with Tailwind CSS
- **State Management**: React hooks with local state
- **API Communication**: Custom ApiService class
- **Authentication**: Supabase Auth with context provider

### Key Components

1. **Task Detail Page** (`app/tasks/[id]/page.tsx`):
   - Real-time status polling
   - Diff visualization
   - PR creation UI
   - Chat message history

2. **API Service** (`lib/api-service.ts`):
   - Centralized API communication
   - Type-safe request/response handling
   - Error management

### Status Polling Implementation

```typescript
// app/tasks/[id]/page.tsx:54-79
useEffect(() => {
    if (!user?.id || !task || (task.status !== "running" && task.status !== "pending")) return;

    const interval = setInterval(async () => {
        try {
            const updatedTask = await ApiService.getTaskStatus(user.id, taskId);
            setTask(prev => ({ ...prev, ...updatedTask }));

            // Fetch git diff if task completed
            if (updatedTask.status === "completed" && !gitDiff) {
                const diff = await ApiService.getGitDiff(user.id, taskId);
                setGitDiff(diff);
            }
        } catch (error) {
            console.error('Error polling task status:', error);
        }
    }, 2000); // Poll every 2 seconds

    return () => clearInterval(interval);
}, [task, user?.id, taskId, gitDiff]);
```

### User Preferences and Credentials

The system supports user-specific configurations:

```python
# utils/code_task_v2.py:145-179
# Get user preferences for custom environment variables
user = DatabaseOperations.get_user_by_id(user_id)
user_preferences = user.get('preferences', {}) if user else {}

if model_cli == 'claude':
    # Merge with user's custom Claude environment variables
    claude_config = user_preferences.get('claudeCode', {})
    if claude_config and claude_config.get('env'):
        claude_env.update(claude_config['env'])
    
    # Load Claude credentials from preferences
    credentials_json = claude_config.get('credentials') if claude_config else None
```

### Diff Visualization

The frontend includes a sophisticated diff viewer component:
- Syntax highlighting
- File-by-file navigation
- Addition/deletion statistics
- Before/after comparison views

## Data Flow

1. **Task Submission**:
   - User submits prompt → API creates task → Background thread starts
   - Task ID returned immediately for tracking

2. **Execution**:
   - Docker container created with appropriate CLI tool
   - Git repository cloned inside container
   - CLI tool executed with prompt
   - Changes committed locally in container

3. **Result Processing**:
   - Container output parsed for patches, diffs, and file changes
   - Results stored in Supabase
   - Container cleaned up

4. **Frontend Updates**:
   - Client polls `/task-status/<id>` endpoint
   - UI updates based on task state
   - Completed tasks show diff and PR creation option

5. **PR Creation**:
   - User triggers PR creation
   - Server applies patch to new branch via GitHub API
   - PR created and linked to task

## Error Handling and Recovery

1. **Container Management**:
   - Orphaned container cleanup before new tasks
   - Unique container naming to prevent conflicts
   - Force removal on failures

2. **Execution Timeouts**:
   - 5-minute timeout for container execution
   - Graceful error reporting on timeout

3. **Resource Conflicts**:
   - Staggered starts for parallel Codex tasks
   - File-based locking for Codex execution
   - Exponential backoff on container creation failures

## Security Considerations

1. **Token Security**:
   - GitHub tokens passed per-request, not stored
   - Tokens used only within containers
   - Container isolation prevents token leakage

2. **Code Execution**:
   - All code execution in Docker containers
   - No direct host system access
   - Resource limits prevent DoS

3. **API Authentication**:
   - User ID required in headers
   - Tasks scoped to authenticated users
   - Supabase Row Level Security

## Configuration and Deployment

1. **Environment Variables**:
   - `ANTHROPIC_API_KEY`: For Claude CLI
   - `OPENAI_API_KEY`: For Codex CLI
   - `SUPABASE_URL` and `SUPABASE_SERVICE_KEY`: Database access

2. **Docker Images**:
   - `claude-code-automation:latest`: Contains Claude CLI
   - `codex-automation:latest`: Contains Codex CLI
   - Both images include git and necessary dependencies

3. **Database Schema**:
   - Tasks table with status tracking
   - User preferences for credentials and environment
   - Execution metadata for detailed results

## Performance Optimizations

1. **Parallel Execution**:
   - Background threads for task execution
   - No blocking of API requests

2. **Efficient Polling**:
   - 2-second intervals only for active tasks
   - Automatic stop on completion/failure

3. **Container Reuse**:
   - Prepared base images with CLI tools
   - Fast container startup times

## Comparison with swe-swe

| Feature | swe-swe | async-code |
|---------|---------|------------|
| Architecture | Monolithic Go binary | Microservices (Flask + Docker) |
| Real-time Updates | WebSocket streaming | HTTP polling |
| CLI Integration | Direct process execution | Docker container isolation |
| Output Format | JSON streaming | Structured text parsing |
| Storage | In-memory | Supabase (persistent) |
| PR Creation | Manual (user creates) | Automated (GitHub API) |
| Multi-model | Goose/Claude presets | Claude/Codex with dynamic selection |
| User Management | None | Full auth with preferences |
| Scale | Single instance | Horizontally scalable |
| Security | Process isolation | Container isolation |

This architecture makes async-code suitable for production deployments with multiple users, while swe-swe is ideal for local development or single-user scenarios.