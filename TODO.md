- Better console outputs per task
- Add dynamic dependencies? For every .c file (except main.c) add a task to compile it to .o, then main depends on all .o files. This way we don't have to manually list all .o files as dependencies of main.
- Integration tests
- Cross compilation support
- Remote caching

P4:

- Remote execution support
- Concurrency, cache dir flag
