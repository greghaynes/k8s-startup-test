k8s-startup-test
================

Testing base startup times for Kubernetes pods


Running the test
----------------

This depends on the
[ko](https://github.com/google/go-containerregistry/tree/master/cmd/ko)
utility.

`ko apply -f config` will build, deploy, and start the test.

View the logs of the startup-test container in the startup-test job to see
the results when the job has completed.


How it works
------------

The tests are spawned from a Kubernetes Job. For each test an HTTP server is
started from within the Job. A timer is started and a pod is created which
performs a HTTP GET to the Job's pod. When the Job's HTTP server recieves this
request it stops the timer. The result is a fairly accurate measurement of time
between calling to the Kubernetes API to start a pod and when that pod begins
executing a users' application.
