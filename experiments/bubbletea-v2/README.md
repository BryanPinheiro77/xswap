# Bubble Tea v2 evaluation prototype

This nested Go module exercises the terminal concerns that matter to XSwap
without changing the production binary:

- arrow-key menu navigation and one-key confirmation;
- Unicode text input, deletion, save, and cancellation;
- window resize messages and asynchronous ticks;
- terminal release and restoration around an interactive child process through
  `tea.ExecProcess`;
- declarative alternate-screen ownership through `tea.View`.

Run it with:

```sh
go test ./...
go run .
```

The Unix integration test requires the standard `script` utility and configures
its otherwise headless pseudo-terminal to 80x24 before starting the prototype.
Windows is cross-compiled here; native Windows terminal behavior remains a
migration validation gate.

Choose **Run terminal input probe…**, enter text at the child prompt, and verify
that the panel returns with `terminal input returned to Bubble Tea`. This is an
evaluation artifact, not part of the XSwap executable.
