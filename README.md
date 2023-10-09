# slogemail

Go's log/slog email handler. Wraps slog's JSONHandler or TextHander.  There are three ways  to create a handler.

### Synchronous handler  
Behaves as a regular handler but also send an email.
By default, email subject is log level and body is log message rendered by base handler. 
This can be customized by setting `GetSubject` and `GetBody` in `slogemail.EmailHandlerOpts`.

```go
h, err := slogemail.NewHandler(
	os.Stdout, 
	&slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}, 
	slogemail.EmailHandlerOpts{
                FromAddr:       "from@example.com",
                ToAddrs:        []string{"one@example.com", "two@example.com"},
                JSON:           true,
                // Level can be different from base handler options
                Level:          slog.LevelError, 
                ConnectionInfo: slogemail.SMTPConnectionInfo{
                        Host:     "example.com",
                        Port:     587,
                        Username: "user",
                        Password: "pwd",
                },
        })
if err != nil {
		panic(err)
}

logger := slog.New(h)
// will printed to stdout and sent to email
logger.Error("database error", "detail", "database has gone away")
// will only be printed
logger.Info("login", "user", os.Getenv("USER"))
```


### Asynchronous handler
The same as synchronous, but sending email is performed in a separate goroutine, so that log handling takes less time for the calling goroutine. The handler should be explicitly stopped by calling a provided function, otherwise some log records may be lost.
```go
h, stop, err := slogemail.NewAsyncHandler(
	os.Stdout, 
	&slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}, 
	slogemail.EmailHandlerOpts{				
				// how many log records can be sent to handler before it starts to process them synchronously
				AsyncQueueSize: 10,
                FromAddr:       "from@example.com",
                ToAddrs:        []string{"one@example.com", "two@example.com"},
                JSON:           true,
                Level:          slog.LevelError, 
                ConnectionInfo: slogemail.SMTPConnectionInfo{
                        Host:     "example.com",
                        Port:     587,
                        Username: "user",
                        Password: "pwd",
                },
        })
if err != nil {
		panic(err)
}

// Needs to be called before application exits so that handler can process remaining logs
defer stop()

logger := slog.New(h)
// will printed to stdout and sent to email
logger.Error("just awful", "err", "database error")
// will only be printed
logger.Info("hello, world", "user", os.Getenv("USER"))
```


### Custom synchronous handler
User defines a function that will handle log record.
```go
h := slogemail.NewCustomHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelDebug},
		func(ctx context.Context, r slog.Record, logOutput string) error {
				doSomething(ctx, logOutput)
		},
		true)

logger := slog.New(h)
```

