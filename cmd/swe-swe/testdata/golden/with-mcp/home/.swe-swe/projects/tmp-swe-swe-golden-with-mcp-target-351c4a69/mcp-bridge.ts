import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";

type JsonRpcRequest = {
  jsonrpc: "2.0";
  id: number;
  method: string;
  params?: unknown;
};

type JsonRpcResponse = {
  id?: number;
  result?: any;
  error?: { code: number; message: string; data?: unknown };
};

type McpTool = {
  name: string;
  description?: string;
  inputSchema?: any;
};

type SessionInfo = {
  uuid?: string;
  workDir?: string;
  agentChatPort?: number;
  AgentChatPort?: number;
};

type McpClient = {
  initialize(): Promise<void>;
  listTools(): Promise<McpTool[]>;
  callTool(name: string, args: Record<string, any>): Promise<any>;
  dispose(): Promise<void>;
};

const DEFAULT_PARAMETERS = {
  type: "object",
  properties: {},
  additionalProperties: true,
  required: [],
} as const;

function env(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

function envNumber(name: string): number | undefined {
  const value = env(name);
  if (!value) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function toolLabel(name: string): string {
  return name.replace(/[_-]+/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

function toolName(serverName: string, remoteName: string): string {
  return `mcp__${serverName}__${remoteName.replace(/[^a-zA-Z0-9]+/g, "_")}`;
}

function normalizeParameters(_inputSchema: any): any {
  return DEFAULT_PARAMETERS;
}

function extractText(result: any): string {
  if (result == null) return "(empty response)";
  if (typeof result === "string") return result;
  if (Array.isArray(result.content)) {
    const parts = result.content
      .map((item: any) => {
        if (!item) return "";
        if (item.type === "text") return item.text ?? "";
        if (item.type === "image") return `[image ${item.mimeType ?? item.mediaType ?? "unknown"}]`;
        if (item.type === "resource") return `[resource ${item.resource?.uri ?? "unknown"}]`;
        return JSON.stringify(item, null, 2);
      })
      .filter(Boolean);
    if (parts.length > 0) return parts.join("\n");
  }
  return JSON.stringify(result, null, 2);
}

function extractStructured(result: any): any {
  if (result == null) return result;
  if (result.result !== undefined) return result.result;
  if (Array.isArray(result.content)) {
    const text = result.content
      .filter((item: any) => item && item.type === "text" && typeof item.text === "string")
      .map((item: any) => item.text)
      .join("\n")
      .trim();
    if (!text) return result;
    try {
      return JSON.parse(text);
    } catch {
      return text;
    }
  }
  return result;
}

function parseMaybeJson(text: string): any {
  const trimmed = text.trim();
  if (!trimmed) return undefined;
  try {
    return JSON.parse(trimmed);
  } catch {
    return undefined;
  }
}

function splitSseData(text: string): string[] {
  const blocks = text.split(/\n\s*\n/);
  const out: string[] = [];
  for (const block of blocks) {
    const data = block
      .split(/\r?\n/)
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).trimStart())
      .join("\n")
      .trim();
    if (data) out.push(data);
  }
  return out;
}

class HttpMcpClient implements McpClient {
  private nextId = 1;
  constructor(private readonly name: string, private readonly endpoint: string) {}

  async initialize(): Promise<void> {
    await this.request("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "pi-mcp-bridge", version: "1.0.0" },
    });
  }

  async listTools(): Promise<McpTool[]> {
    const response = await this.request("tools/list", {});
    const tools = Array.isArray(response?.result?.tools) ? response.result.tools : Array.isArray(response?.tools) ? response.tools : [];
    return tools as McpTool[];
  }

  async callTool(name: string, args: Record<string, any>): Promise<any> {
    const response = await this.request("tools/call", { name, arguments: args });
    return response?.result ?? response?.content ?? response;
  }

  async dispose(): Promise<void> {
    // stateless
  }

  private async request(method: string, params?: unknown): Promise<JsonRpcResponse> {
    const id = this.nextId++;
    const payload: JsonRpcRequest = { jsonrpc: "2.0", id, method, params };
    let lastError: unknown;

    for (let attempt = 0; attempt < 30; attempt++) {
      try {
        const response = await fetch(this.endpoint, {
          method: "POST",
          headers: { "content-type": "application/json", accept: "application/json, text/event-stream" },
          body: JSON.stringify(payload),
        });

        const text = await response.text();
        if (!response.ok) {
          throw new Error(`HTTP ${response.status} ${response.statusText}: ${text.slice(0, 200)}`);
        }

        const trimmed = text.trim();
        let parsed = parseMaybeJson(trimmed);
        if (!parsed) {
          const sse = splitSseData(trimmed).map(parseMaybeJson).filter(Boolean) as JsonRpcResponse[];
          parsed = sse.find((m) => m.id === id && (m.result !== undefined || m.error)) ?? sse.find((m) => m.result !== undefined || m.error);
        }
        if (!parsed && trimmed.startsWith("<")) {
          throw new Error(`HTTP MCP at ${this.endpoint} returned HTML: ${trimmed.slice(0, 120)}`);
        }
        if (!parsed && trimmed.split(/\r?\n/).length > 1) {
          const lines = trimmed.split(/\r?\n/).map((line) => parseMaybeJson(line)).filter(Boolean) as JsonRpcResponse[];
          parsed = lines.find((m) => m.id === id && (m.result !== undefined || m.error)) ?? lines.find((m) => m.result !== undefined || m.error);
        }
        if (!parsed) {
          throw new Error(`Unable to parse MCP response from ${this.endpoint}: ${trimmed.slice(0, 200)}`);
        }
        if (parsed.error) throw new Error(parsed.error.message);
        return parsed;
      } catch (error) {
        lastError = error;
        const msg = error instanceof Error ? error.message : String(error);
        if (!/fetch failed|ECONNREFUSED|socket hang up|timed out|returned HTML|Unable to parse MCP response/i.test(msg) || attempt === 29) {
          throw error;
        }
        await sleep(250);
      }
    }

    throw lastError instanceof Error ? lastError : new Error(String(lastError));
  }
}

class StdioMcpClient implements McpClient {
  private nextId = 1;
  private proc?: ChildProcessWithoutNullStreams;
  private pending = new Map<number, { resolve: (r: JsonRpcResponse) => void; reject: (e: Error) => void }>();
  private stdoutBuffer = "";
  private startError?: Error;
  private exited = false;

  constructor(
    private readonly name: string,
    private readonly command: string,
    private readonly args: string[],
    private readonly extraEnv: NodeJS.ProcessEnv = {},
  ) {}

  private ensureStarted(): void {
    if (this.proc || this.startError) return;
    const child = spawn(this.command, this.args, {
      env: { ...process.env, ...this.extraEnv },
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.proc = child;

    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => this.onStdout(chunk));
    child.stderr.setEncoding("utf8");
    child.stderr.on("data", (chunk: string) => {
      for (const line of chunk.split(/\r?\n/)) {
        if (line.trim()) console.error(`[mcp:${this.name}] ${line}`);
      }
    });

    child.on("error", (err) => {
      this.startError = err;
      this.failAllPending(err);
    });

    child.on("exit", (code, signal) => {
      this.exited = true;
      console.error(`[mcp:${this.name}] exited code=${code ?? "?"} signal=${signal ?? "?"}`);
      this.failAllPending(new Error(`MCP server ${this.name} exited code=${code ?? "?"} signal=${signal ?? "?"}`));
    });
  }

  private onStdout(chunk: string): void {
    this.stdoutBuffer += chunk;
    while (true) {
      const nl = this.stdoutBuffer.indexOf("\n");
      if (nl < 0) break;
      const line = this.stdoutBuffer.slice(0, nl).trim();
      this.stdoutBuffer = this.stdoutBuffer.slice(nl + 1);
      if (!line) continue;
      const msg = parseMaybeJson(line) as JsonRpcResponse | undefined;
      if (!msg || typeof msg.id !== "number") continue;
      const handler = this.pending.get(msg.id);
      if (!handler) continue;
      this.pending.delete(msg.id);
      handler.resolve(msg);
    }
  }

  private failAllPending(err: Error): void {
    const entries = [...this.pending.entries()];
    this.pending.clear();
    for (const [, handler] of entries) handler.reject(err);
  }

  async initialize(): Promise<void> {
    this.ensureStarted();
    await this.request("initialize", {
      protocolVersion: "2024-11-05",
      capabilities: {},
      clientInfo: { name: "pi-mcp-bridge", version: "1.0.0" },
    });
    this.notify("notifications/initialized", {});
  }

  async listTools(): Promise<McpTool[]> {
    const response = await this.request("tools/list", {});
    const tools = Array.isArray(response?.result?.tools) ? response.result.tools : Array.isArray((response as any)?.tools) ? (response as any).tools : [];
    return tools as McpTool[];
  }

  async callTool(name: string, args: Record<string, any>): Promise<any> {
    const response = await this.request("tools/call", { name, arguments: args });
    return response?.result ?? (response as any)?.content ?? response;
  }

  async dispose(): Promise<void> {
    if (!this.proc || this.exited) return;
    this.proc.kill();
  }

  private notify(method: string, params: unknown): void {
    if (!this.proc || this.exited) return;
    const payload = { jsonrpc: "2.0", method, params };
    try {
      this.proc.stdin.write(JSON.stringify(payload) + "\n");
    } catch {
      // ignore; next request will surface the failure
    }
  }

  private request(method: string, params?: unknown): Promise<JsonRpcResponse> {
    if (this.startError) return Promise.reject(this.startError);
    if (!this.proc || this.exited) return Promise.reject(new Error(`MCP server ${this.name} is not running`));

    const id = this.nextId++;
    const payload: JsonRpcRequest = { jsonrpc: "2.0", id, method, params };
    return new Promise<JsonRpcResponse>((resolve, reject) => {
      const timer = setTimeout(() => {
        if (this.pending.delete(id)) {
          reject(new Error(`MCP ${this.name} ${method} timed out`));
        }
      }, 60_000);
      timer.unref();

      this.pending.set(id, {
        resolve: (r) => {
          clearTimeout(timer);
          if (r.error) reject(new Error(r.error.message));
          else resolve(r);
        },
        reject: (e) => {
          clearTimeout(timer);
          reject(e);
        },
      });

      try {
        this.proc!.stdin.write(JSON.stringify(payload) + "\n");
      } catch (err) {
        this.pending.delete(id);
        clearTimeout(timer);
        reject(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }
}

class SpawnedHttpService {
  private proc?: ChildProcessWithoutNullStreams;
  private readyPromise?: Promise<string>;
  private resolveReady?: (url: string) => void;
  private rejectReady?: (err: Error) => void;
  private readyUrl?: string;
  private killed = false;

  constructor(private readonly spec: { name: string; command: string; args: string[]; env?: NodeJS.ProcessEnv; portRegex: RegExp }) {}

  start(): Promise<string> {
    if (this.readyPromise) return this.readyPromise;
    this.readyPromise = new Promise((resolve, reject) => {
      this.resolveReady = resolve;
      this.rejectReady = reject;
    });

    this.proc = spawn(this.spec.command, this.spec.args, {
      env: { ...process.env, ...this.spec.env },
      stdio: ["ignore", "ignore", "pipe"],
    });

    const timeout = setTimeout(() => this.fail(new Error(`Timed out waiting for ${this.spec.name} to start`)), 30_000);
    timeout.unref();

    this.proc.stderr.on("data", (chunk: Buffer) => {
      for (const line of chunk.toString("utf8").split(/\r?\n/)) {
        if (!line.trim()) continue;
        console.error(`[mcp:${this.spec.name}] ${line}`);
        const match = line.match(this.spec.portRegex);
        if (match) {
          this.readyUrl ??= match[1];
          this.resolveReady?.(this.readyUrl);
          clearTimeout(timeout);
        }
      }
    });

    this.proc.on("exit", (code, signal) => {
      clearTimeout(timeout);
      if (!this.readyUrl) this.fail(new Error(`MCP service ${this.spec.name} exited before ready: code=${code ?? "?"} signal=${signal ?? "?"}`));
    });

    this.proc.on("error", (err) => {
      clearTimeout(timeout);
      this.fail(err);
    });

    return this.readyPromise;
  }

  async dispose(): Promise<void> {
    this.killed = true;
    if (!this.proc) return;
    this.proc.kill();
  }

  private fail(err: Error): void {
    if (this.killed) return;
    this.killed = true;
    this.rejectReady?.(err);
  }
}

class McpBridge {
  private clients = new Map<string, McpClient>();
  private registeredTools = new Set<string>();

  async start(pi: ExtensionAPI, ctx: any): Promise<void> {
    await this.stop();
    this.registeredTools.clear();

    const session = await this.getSessionInfo();
    const endpoints: Array<{ name: string; client: McpClient }> = [];

    const sweServerPort = envNumber("SWE_SERVER_PORT") ?? 1977;
    const sweAuthKey = env("MCP_AUTH_KEY");
    const sessionUuid = env("SESSION_UUID") ?? session?.uuid ?? "";

    // swe-swe core: HTTP MCP served directly by swe-swe-server
    endpoints.push({
      name: "swe-swe",
      client: new HttpMcpClient("swe-swe", `http://localhost:${sweServerPort}/mcp${sweAuthKey ? `?key=${sweAuthKey}` : ""}`),
    });

    // swe-swe-preview: agent-reverse-proxy MCP served by swe-swe-server at
    // /proxy/<uuid>/preview/mcp. authMiddleware key-exempts this path (via
    // proxyPreviewMCPPath + sessionKeyMatchesPath), so the per-session
    // MCP_AUTH_KEY authorizes it -- same scheme as /mcp and browser/start.
    // Requires SESSION_UUID; without it we can't build the scoped path.
    if (sessionUuid) {
      endpoints.push({
        name: "swe-swe-preview",
        client: new HttpMcpClient(
          "swe-swe-preview",
          `http://localhost:${sweServerPort}/proxy/${sessionUuid}/preview/mcp${sweAuthKey ? `?key=${sweAuthKey}` : ""}`,
        ),
      });
    }

    // swe-swe-agent-chat: spawn agent-chat with --no-stdio-mcp so it serves
    // MCP over HTTP, then bridge to it. The agent-chat process also serves the
    // chat UI on AGENT_CHAT_PORT (default 4000).
    //
    // AGENT_CHAT_DISABLE must be explicitly cleared. swe-swe-server sets it to
    // "1" for the pi session subprocess so the *parent* doesn't auto-start its
    // own agent-chat UI -- but with `--no-stdio-mcp` (HTTP-only mode), the
    // env flag short-circuits the HTTP server too and agent-chat starts
    // nothing. The other agents (claude/codex/gemini/goose) don't hit this
    // because they run agent-chat in stdio MCP mode where the flag only
    // suppresses the UI side.
    const agentChatPort = session?.agentChatPort ?? session?.AgentChatPort ?? envNumber("AGENT_CHAT_PORT") ?? 4000;
    const agentChatService = new SpawnedHttpService({
      name: "agent-chat",
      command: "npx",
      args: [
        "-y",
        "@choonkeat/agent-chat",
        "--no-stdio-mcp",
        "--theme-cookie",
        "swe-swe-theme",
        "--autocomplete-triggers",
        "/=slash-command",
        "--autocomplete-url",
        `http://localhost:${sweServerPort}/api/autocomplete/${sessionUuid}?key=${sweAuthKey ?? ""}`,
      ],
      env: { AGENT_CHAT_PORT: String(agentChatPort), AGENT_CHAT_DISABLE: "" },
      portRegex: /Agent Chat UI:\s+(http:\/\/localhost:(\d+))/,
    });

    // Whiteboard inherits PORT from pi (set to the preview port by swe-swe-
    // server, e.g. 3200), which whiteboard would happily bind -- conflicting
    // with whatever else expects the preview port. Clear PORT so whiteboard
    // falls back to its own default.
    const whiteboardService = new SpawnedHttpService({
      name: "whiteboard",
      command: "npx",
      args: ["-y", "@choonkeat/agent-whiteboard", "--no-stdio-mcp"],
      env: { PORT: "" },
      portRegex: /Agent Whiteboard UI:\s+(http:\/\/localhost:(\d+))/,
    });

    const agentChatUrl = await agentChatService.start().catch((err) => {
      ctx.ui?.notify?.(`MCP bridge agent-chat: ${err.message}`, "warning");
      return undefined;
    });
    if (agentChatUrl) {
      endpoints.push({ name: "swe-swe-agent-chat", client: new HttpMcpClient("agent-chat", `${agentChatUrl}/mcp`) });
    }

    const whiteboardUrl = await whiteboardService.start().catch((err) => {
      ctx.ui?.notify?.(`MCP bridge whiteboard: ${err.message}`, "warning");
      return undefined;
    });
    if (whiteboardUrl) {
      endpoints.push({ name: "swe-swe-whiteboard", client: new HttpMcpClient("whiteboard", `${whiteboardUrl}/mcp`) });
    }

    // swe-swe-playwright: wrap @playwright/mcp in mcp-lazy-init so the
    // per-session browser only starts on the first tools/call, matching the
    // lazy behavior used by the other agent configs.
    const browserCdpPort = envNumber("BROWSER_CDP_PORT");
    if (sessionUuid && browserCdpPort) {
      endpoints.push({
        name: "swe-swe-playwright",
        client: new StdioMcpClient("playwright", "mcp-lazy-init", [
          "--init-method",
          "POST",
          "--init-url",
          `http://localhost:${sweServerPort}/api/session/${sessionUuid}/browser/start?key=${sweAuthKey ?? ""}`,
          "--",
          "npx",
          "-y",
          "@playwright/mcp@latest",
          "--cdp-endpoint",
          `http://localhost:${browserCdpPort}`,
        ]),
      });
    }

    let loaded = 0;
    for (const spec of endpoints) {
      try {
        await spec.client.initialize();
        const tools = await spec.client.listTools();
        this.clients.set(spec.name, spec.client);

        for (const remoteTool of tools) {
          const piTool = toolName(spec.name, remoteTool.name);
          if (this.registeredTools.has(piTool)) continue;
          this.registeredTools.add(piTool);
          loaded++;

          const client = spec.client;
          const remoteName = remoteTool.name;
          pi.registerTool({
            name: piTool,
            label: toolLabel(remoteName),
            description: remoteTool.description ? `${spec.name}: ${remoteTool.description}` : `${spec.name}: ${remoteName}`,
            parameters: normalizeParameters(remoteTool.inputSchema),
            async execute(_toolCallId, params) {
              const result = await client.callTool(remoteName, (params ?? {}) as Record<string, any>);
              return {
                content: [{ type: "text", text: extractText(result) }],
                details: extractStructured(result),
              };
            },
          });
        }
      } catch (error: any) {
        ctx.ui?.notify?.(`MCP bridge ${spec.name}: ${error?.message ?? String(error)}`, "warning");
      }
    }

    ctx.ui?.notify?.(`MCP bridge loaded ${loaded} tool(s)`, "info");
  }

  async stop(): Promise<void> {
    const clients = [...this.clients.values()];
    this.clients.clear();
    await Promise.allSettled(clients.map((client) => client.dispose()));
  }

  private async getSessionInfo(): Promise<SessionInfo | undefined> {
    try {
      const port = envNumber("SWE_SERVER_PORT") ?? 1977;
      const key = env("MCP_AUTH_KEY");
      const client = new HttpMcpClient("swe-swe", `http://localhost:${port}/mcp${key ? `?key=${key}` : ""}`);
      await client.initialize();
      const response = await client.callTool("list_sessions", {});
      const payload = extractStructured(response);
      const sessions = Array.isArray(payload)
        ? payload
        : Array.isArray(payload?.sessions)
          ? payload.sessions
          : Array.isArray(payload?.result?.sessions)
            ? payload.result.sessions
            : [];
      return (
        sessions.find((s: SessionInfo) => s.uuid && env("SESSION_UUID") && s.uuid === env("SESSION_UUID")) ??
        sessions.find((s: SessionInfo) => s.workDir && s.workDir === process.cwd()) ??
        sessions[0]
      );
    } catch {
      return undefined;
    }
  }
}

const bridge = new McpBridge();

export default function (pi: ExtensionAPI) {
  pi.on("session_start", async (_event, ctx) => {
    await bridge.start(pi, ctx);
  });

  pi.on("session_shutdown", async () => {
    await bridge.stop();
  });
}
