# Anarchy-Kitten

This app tests Route53 failover given a set of servers in an AWS auto scaling group.
## How to run:


```bash
$ go build
$ AWS_ACCESS_KEY_ID=true AWS_SECRET_ACCESS_KEY=true AWS_REGION=true ASG_NAME=prod ./Anarchy-Kitten 
```

Where `ASG_NAME` is the name of the autoscaling group.

The following command line flags are also available:

```
 -url=http://www.mywebsite.com
The URL to check for content
 
 -content=Maintenance
The known content to check for after failover

 -timeout=600
The number of seconds to timeout after when waiting for an instance to change lifecycle state in the autoscaling group

 -poll=10
The number of seconds to poll for when waiting for an instance to change Lifecycle state in the autoscaling group

 ```
