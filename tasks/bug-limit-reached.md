a message

```
2025/07/17 16:37:54 [STDOUT] {"type":"assistant","message":{"id":"bcc0455c-6766-45d6-9c55-42df4f609d96","model":"<synthetic>","role":"assistant","stop_reason":"stop_sequence","stop_sequence":"","type":"message","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"server_tool_use":{"web_search_requests":0},"service_tier":null},"content":[{"type":"text","text":"Claude AI usage limit reached|1752757200"}]},"parent_tool_use_id":null,"session_id":"3960f2f1-6813-4872-9acb-1034aa01c3eb"}
```

we should parse the `1752757200` text into timestamp in local timezone
