# Anarchy-Kitten

This app tests Route53 failover given a set of servers in an AWS auto scaling group.
## How to run:


```bash
$ go build
$ AWS_ACCESS_KEY_ID=true AWS_SECRET_ACCESS_KEY=true AWS_REGION=true ASG_NAME=prod ./Anarchy-Kitten 
```

Where `ASG_NAME` is the name of the autoscaling group.

Configuration options defined in `config.yaml` residing in the same directory as the binary. See `confif-example.yaml` for examples and documentation.
