# Anarchy-Kitten

## How to run:

```bash
$ AWS_ACCESS_KEY_ID=true AWS_SECRET_ACCESS_KEY=true AWS_REGION=true go run main.go
```

## Development Gameplan!

- [ ] Plan how to test
- [ ] Get autoscaling group
- [ ] Put each into stand by one by one
- [ ] Once all are in standby
- [ ] Poll domain name for a time provided in config to wait for:
	- [ ] 200 HTTP status
	- [ ] Known status page content
- [ ] Report to the user whether it passed or not
- [ ] Regardless of outcome, put all servers back into ASG
