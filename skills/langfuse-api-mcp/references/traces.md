# Traces, sessions and errors

A trace is one run of the application; its observations (spans, generations, events) form a tree. A session groups many traces, and a user owns many traces. Each has its own ID.

**A session ID is never a trace ID.** `get_trace_tree` takes one trace ID. For a session or a user, list observations with the session or user filter.

| The user asks for | Call |
|---|---|
| one trace, by its trace ID | `get_trace_tree` with that `traceId`: the whole tree in one call |
| a session's observations or traces | `execute_read` `observations_getMany` with `sessionId` set to the session ID |
| a user's observations or traces | `execute_read` `observations_getMany` with `userId` set to the user ID |
| errors | `execute_read` `observations_getMany` with `level` `ERROR` in a bounded window; group the rows by `traceId`, then open each failing trace with `get_trace_tree` |

- For one row per trace (a session's or a user's traces, not every span), add `isRootObservation` `true`: the root observation carries the trace's input and output.
- Rows come newest first. Reverse them to replay a session in order.
- A page may end with a cursor in `data.meta.cursor`. To read on, call `execute_read` again with the same parameters plus that `cursor`, unchanged.
- Confirm each parameter name and format with `describe_operation observations_getMany` before the first call.
