# Spec-Driven Vibe Coding With Typed Elm Modules

_A proposal for using Elm's type system as a living, executable specification for distributed systems, UIs, and data flows._

## 1. Overview

This document introduces a development workflow in which **Elm modules** serve as **executable specifications** for a system's behavior, data flows, and UI structure.

Instead of writing prose documentation or informal plans, the developer (or an AI agent) writes:

- **type aliases**
- **record types**
- **union types**
- **function signatures**
- **minimal function bodies calling "effects"**

These files **compile**, even though they contain _no real implementation_, ensuring the specification stays structurally correct.

This creates a foundation for:

- consistent feature development
- safe vibe-coding
- automated code generation
- machine-verifiable evolution of requirements
- early detection of missing data dependencies

This workflow can be run by:

- a **primary coding agent** (that writes production code)
- a **spec agent** (that maintains the Elm specification)
- or a single agent performing both tasks in stages

## 2. What Problems This Solves

### 2.1 Prose specifications are ambiguous

Human-written documentation and requirements drift out of sync with code.

### 2.2 "Vibe coding" lacks constraints

AI or human code generation often misses subtle requirements.

### 2.3 Missing data dependencies

E.g., "add payment" function takes only an amount, but real payment requires:

- payment method
- payer
- currency
- authorization

These omissions are invisible until too late.

### 2.4 Hard to track UI -> server -> storage data flows

Especially when pages require certain data inputs that come from user entry.

## 3. Core Idea

### 3.1 Write the system spec as Elm modules

Each module describes either:

- A **client** (UI pages, user actions)
- A **server** (API endpoints, domain operations)
- A **shared domain** (types used by both sides)

These modules live in a `./tdspec/` directory at the project root:

```
./tdspec/
  elm.json
  src/
    Domain.elm          -- shared types
    ExpenseClient.elm   -- client spec
    ExpenseServer.elm   -- server spec
```

The `./tdspec/src/` directory contains all `.elm` spec files. This keeps the spec isolated from production code while living alongside it in the same repository.

These modules export **only function signatures and types**.

Example:

```elm
addExpense : AddExpensePayload -> ServerMsg
```

### 3.2 Use opaque wrapper types, not bare aliases

A bare type alias like:

```elm
type alias Url = String
```

provides **zero type safety** -- the compiler treats `Url` and `String` as interchangeable, so a raw `String` can silently pass where a `Url` is expected.

Instead, use **opaque wrapper types**:

```elm
type Url = Url String
type Email = Email String
type Amount = Amount Float
```

This forces the caller to explicitly construct and destructure values, which means:

- A `String` cannot accidentally flow where a `Url` is expected
- The compiler catches mix-ups between semantically different values (e.g., passing an `Email` where a `Url` is required)
- Domain boundaries are enforced at the type level

This is central to the spec's purpose. If the types are not distinct, the compiler cannot catch misuse, and the specification loses its verification power.

### 3.3 Each function signature represents a protocol message

The **function name** becomes the message/operation name.

The **arguments** become the payload.

The **return type** becomes all possible outcomes.

### 3.4 Minimal Elm function bodies enforce data completeness

Instead of:

```elm
addExpense payload =
    Debug.todo "later"
```

You write:

```elm
addExpense payload =
    ChargePayment { amount = payload.amount, method = payload.method }
```

This forces the spec to supply **all required raw fields**.

If you forget a field (e.g., method), Elm fails to compile.

This is the key to catching design gaps early.

### 3.5 Pages on the client side are expressed as typed return values

A page is represented by a coarse structural type, such as:

```elm
type alias Page =
    { title : String
    , actions : List Action
    , inputs : List InputField
    }
```

Example for a name-editing page:

```elm
editNamePage : Page
```

It describes:

- what inputs are shown
- what actions are available
- what data must flow to the next page

### 3.6 Named type variables with comments

When a type has multiple type parameters, use descriptive parameter names in the definition and inline comments at call sites:

```elm
-- Good: definition names the roles, comments label each argument
type WebSocketChannel server serverMsg client clientMsg
    = WebSocketChannel

ptyChannel :
    WebSocketChannel
        SweServer                 -- server
        PtyProtocol.ServerMsg     -- serverMsg
        TerminalUi                -- client
        PtyProtocol.ClientMsg     -- clientMsg
```

The type definition `server serverMsg client clientMsg` is the legend. Comments at call sites are a convenience. Avoid cramming all parameters onto one line where the reader must count positions:

```elm
-- Avoid: positional parameters without comments
ptyChannel : WebSocketChannel SweServer PtyProtocol.ServerMsg TerminalUi PtyProtocol.ClientMsg
```

### 3.7 Precision over generality

Use process-specific types so invalid combinations are unrepresentable:

```elm
-- Good: only valid server/client combinations type-check
type TerminalUi = TerminalUi { label : String, sessionUuid : SessionUuid }
type SweServer = SweServer

ptyChannel :
    WebSocketChannel
        SweServer                 -- server
        PtyProtocol.ServerMsg     -- serverMsg
        TerminalUi                -- client
        PtyProtocol.ClientMsg     -- clientMsg
```

compared to a generic `Process` union where any variant could appear in any role:

```elm
-- Avoid: allows nonsensical combinations like server=Traefik, client=McpSidecar
type Process = BrowserTerminalUi | HostTraefik | ContainerMcpSidecar | ...

ptyChannel :
    { server : Process, client : Process, ... }
```

When each role has its own type, the compiler rejects impossible connections at definition time rather than at runtime.

### 3.8 Records over currying

Use a record argument instead of multiple positional arguments. Currying is idiomatic Elm but requires the reader to understand partial application and remember argument order:

```elm
-- Good: each argument is named
previewProxyPort : { offset : PortOffset, appPort : PreviewPort } -> PreviewProxyPort

onPageLoad : { url : Url, now : Timestamp } -> ShellPageEffect
```

compared to curried positional arguments:

```elm
-- Avoid: reader must remember argument order
previewProxyPort : PortOffset -> PreviewPort -> PreviewProxyPort

onPageLoad : Url -> Timestamp -> ShellPageEffect
```

This spec is not runtime code — clarity for unfamiliar readers matters more than partial application convenience.

### 3.9 Exact types per client

When two clients use the same channel but handle different message subsets, give each client its own types. A shared union that each client partially ignores hides the real protocol:

```elm
-- Avoid: both channels look identical, but shell page and inject.js
-- handle completely different messages
type IframeCommand = IframeNavigate | IframeReload | IframeQuery
type AllDebugMsg = Init | UrlChange | Console | Error | ...

debugIframeShellPage : WebSocketChannel AgentReverseProxy IframeCommand ShellPage AllDebugMsg
debugIframeInjectJs  : WebSocketChannel AgentReverseProxy IframeCommand InjectJs  AllDebugMsg
```

```elm
-- Good: each channel shows exactly what it carries
type ShellPageCommand = ShellNavigate NavigateAction | ShellReload
type ShellPageDebugMsg = Init ... | UrlChange ... | NavState ...

type InjectCommand = DomQuery { id : String, selector : String }
type InjectJsDebugMsg = Console ... | Error ... | Fetch ... | ...

debugIframeShellPage : WebSocketChannel AgentReverseProxy ShellPageCommand ShellPage ShellPageDebugMsg
debugIframeInjectJs  : WebSocketChannel AgentReverseProxy InjectCommand   InjectJs  InjectJsDebugMsg
```

The rule: if a function ignores variants with catch-all branches (`_ ->` or no-op cases), the type is too broad. Split it so each client's type contains only the variants it actually sends or receives.

Exception: a handler that genuinely receives all variants over a single connection (e.g., terminal-ui receives every `AllDebugMsg` on WS 3/4) is correctly typed even if it only acts on a subset — the no-op branches document what it *chooses not to act on*, not what it *cannot receive*.

### 3.10 Names should locate types in a family

When types are related, make the relationship visible in the name. A shared suffix or prefix tells the reader they belong together without requiring them to read the definitions:

```elm
-- Avoid: three unrelated-looking names
type DebugMsg = ...
type ShellPageMsg = ...
type InjectMsg = ...
```

```elm
-- Good: shared suffix reveals the family
type AllDebugMsg = FromShellPage ShellPageDebugMsg | FromInject InjectJsDebugMsg | Open ...
type ShellPageDebugMsg = Init ... | UrlChange ... | NavState ...
type InjectJsDebugMsg = Console ... | Error ... | Fetch ... | ...
```

The suffix `DebugMsg` is the family name. The prefix (`All`, `ShellPage`, `InjectJs`) is the scope. A reader scanning type names can immediately see these are variants of the same concept.

### 3.11 Eliminate duplicate structure

When two types have the same shape, they are the same type. The distinction belongs one level up — at the variant that holds the value, not inside the value itself:

```elm
-- Avoid: identical structure, different names
type FetchResult
    = FetchOk { url : Url, method : String, status : Int, ms : Int, ts : Timestamp }
    | FetchFailed { url : Url, method : String, error : String, ms : Int, ts : Timestamp }

type XhrResult
    = XhrOk { url : Url, method : String, status : Int, ms : Int, ts : Timestamp }
    | XhrFailed { url : Url, method : String, error : String, ms : Int, ts : Timestamp }
```

```elm
-- Good: one type, distinction lives at the call site (Fetch vs Xhr)
type HttpResult
    = HttpResult
        { request : { url : Url, method : String, ms : Int, ts : Timestamp }
        , response : Result { error : String } { httpStatus : Int }
        }
```

Use `Result` when the shape is success-or-failure — do not reinvent it with `Ok`/`Failed` variants.

### 3.12 Nest records to separate roles

A flat record mixes fields from different phases of an interaction. Nesting separates what was asked from what came back:

```elm
-- Avoid: flat bag — which fields are about the request vs the response?
{ url : Url, method : String, status : Int, error : String, ms : Int, ts : Timestamp }
```

```elm
-- Good: nesting names the roles
{ request : { url : Url, method : String, ms : Int, ts : Timestamp }
, response : Result { error : String } { httpStatus : Int }
}
```

Each level of nesting is a claim about structure. If fields always travel together and belong to the same concept, group them.

### 3.13 "Effects" represent server calls, storage operations, notifications, etc.

An **Effect** type enumerates operations requiring raw data:

```elm
type Effect
    = ChargePayment PaymentPayload
    | SendEmail EmailPayload
    | SaveExpense ExpensePayload
```

Client-side and server-side spec functions must produce these effects in their bodies.

This ties high-level operations to the low-level data they require.

## 4. Example: Simple Expense Tracking App

We model a Splitwise-like app with:

- participants
- expenses
- pages where users enter data
- server processes to store and compute splits

### 4.1 Shared Types

```elm
type alias Participant =
    { name : String
    , pax : Float
    }

type alias Expense =
    { description : String
    , amount : Float
    , splitAmong : List Participant
    }
```

### 4.2 Client Module: ExpenseClient

```elm
module ExpenseClient exposing (mainPage, addParticipantPage, addExpensePage)

type alias Page =
    { title : String
    , inputs : List InputField
    , actions : List Action
    }

type InputField
    = TextInput { label : String }

type Action
    = Submit { to : ServerAction }
    | GoTo Page
```

#### Pages

```elm
mainPage : Page
mainPage =
    { title = "Group Expenses"
    , inputs = []
    , actions =
        [ GoTo addParticipantPage
        , GoTo addExpensePage
        ]
    }
```

```elm
addParticipantPage : Page
addParticipantPage =
    { title = "Add Participant"
    , inputs =
        [ TextInput { label = "Name" }
        , TextInput { label = "Pax (e.g., 1.0 or 1.5)" }
        ]
    , actions =
        [ Submit { to = AddParticipant } ]
    }
```

```elm
addExpensePage : Page
addExpensePage =
    { title = "Add Expense"
    , inputs =
        [ TextInput { label = "Description" }
        , TextInput { label = "Amount" }
        ]
    , actions =
        [ Submit { to = AddExpense } ]
    }
```

### 4.3 Server Module: ExpenseServer

```elm
module ExpenseServer exposing (addParticipant, addExpense)
```

#### Function signatures as protocol signals

```elm
addParticipant : { name : String, pax : Float } -> ServerMsg
addExpense : { description : String, amount : Float } -> ServerMsg
```

#### Minimal bodies that enforce required inputs

```elm
addParticipant payload =
    SaveParticipant payload
```

```elm
addExpense payload =
    SaveExpense payload
```

#### Effects

```elm
type Effect
    = SaveParticipant { name : String, pax : Float }
    | SaveExpense { description : String, amount : Float }
```

## 5. Multi-Agent Workflow

### 5.1 Agent A: Spec Agent

- Takes the user's description of a feature
- Updates or extends the Elm spec modules
- Ensures they compile
- Surfaces missing data or contradictions to the user
- Produces structured, compiler-verified requirements

### 5.2 Agent B: Implementation Agent

- Reads the spec
- Generates the actual UI or server code in the target stack
- Periodically rechecks alignment with the Elm spec

### 5.3 Continuous alignment

- When requirements change, update the Elm spec first
- Regenerate code, check for drift
- Compiler errors in spec highlight missing or inconsistent data

## 6. Benefits

### 6.1 Guarantees data completeness

If an effect requires a field, the spec must supply it.

### 6.2 Guarantees protocol consistency

Function signatures encode expected responses.

### 6.3 Enforces UI-server alignment

The shape of pages and actions is typed.

### 6.4 Evolves safely

Changing a type (e.g., adding pax : Float) forces updates everywhere.

### 6.5 Feeds AI agents perfectly

LLMs thrive with strongly typed structure rather than raw prose.

## 7. Summary

This workflow turns Elm into:

- a **living architecture document**
- a **protocol definition**
- a **UI flow blueprint**
- a **dataflow validator**
- a **compiler-verified contract** between client, server, and agent

By defining:

- types
- function signatures
- minimal effectful function bodies
- page structures

We obtain a full specification that is:

- readable
- precise
- type-checked
- evolvable
- a perfect foundation for AI-assisted coding

This approach gives us **spec-driven vibe coding** -- flexible creativity guided by hard guarantees.
