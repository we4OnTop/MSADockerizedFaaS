# MSADockerizedFaaS

We chose as an exercise the creation of a Function-as-a-Service Orchestrator using Docker containers to run serverless functions. For that, different kinds of technologies were used at the end to enable the implementation of

- A mechanism to define functions that should be run when needed
- Some kind of routing depending on the function the user wants to access
- A scaling and load-balancing mechanism to create new containers and destroy existing ones, depending on the connections the current one has
- A global orchestration mechanism to connect everything

In the end a lot of different kinds of implementation and configuration needed to happen to make everything work together. 

## Architecture

![Architecture.png](Architecture.png)

The used Architecture consists of different kind of Technologies representing a specific usage to enable the goals set for this project.
To understand the functionality we are going over every component used to understand its needing and functionality.

1. __Nginx:__ Nginx is used in this project to create a gateway to the whole application. The problem is that starting an container dynamicly results in different kind of problems, for example if the container is not running while a request is sent its not processed and an error is sent back to the user. Also it functions as an trigger, where if no container is running Nginx is notifing the orchestrator over Redis to setup a new one. To enable the possiblity to configure this in Nginx OpenResty is used. OpenResty is an nginx distribution which includes the LuaJIT interpreter for Lua scripts which we use to program the functionality described.
2. __Envoy:__ The reason why Envoy is used as Router and Nginx is only a gate for the application is because of the lacing dynamic pushing of new routes in Nginx. Because of that we are using Envoy with the go connection plane to dynamicly push new listeners, clusters and routes inside of envoy. Envoy functions as main router to route requests to specificly generated docker containers. Also Envoy is used to detekt when an new container needs to be started because of too high usage of the current exisiting.
3. __xDs Connector:__ The controller of Envoy is the xDs Connector. It uses the Go control plane from envoy to dynamcly push changes into the Router. To enable an easy communication between the xDs Connector and Orchestrator an REST-Server was implemented and specific endpoints to push, modify or delete routes, clusters or listeners created. Also the xDs Connector is communicating with Redis to send data about the usage of an container and help to decide when stopping and destroying an container is best to save ressources.
4. __statsD Listener:__ Because under load new containers should be created statsD is used to capture UDP sended telemetry data by envoy to find out when an queue on an cluster is active because of too much connections and all in all help to tell the orchestrator, over redis, to launch new containers into this cluster.
5. __Redis Server:__ Redis is used as an minimal database and an messager between the different components. Because information about running containers, when they should be removed or new should be launched can be set by different sources redis was used.
6. __Redis REST Connector:__ Because Nginx and the lua script needed to talk to Redis also and no right dependency was found, an minimal REST-Server was implemented using GO to use basic needed actions inside of Nginx.
7. __Orchestrator:__ The Brain of the whole application is the Orchestrator, it uses Python and implements the functionality to read information gathered by the different sources in redis and depending on that launch new containers and push the configuration to the needed components like the xDs Connector. 
8. __FAAS Configurator:__ To create the functions that should be runned serverless in the architecture an pre defined setup was created. In this currently only Python is supported. This can be used to define an function as an Python file that can be talked to over REST. An mini-tutorial how to configure new functions is also inside of the ReadMe at the bottom.

Everything is running in docker, in seperate containers and depending on the dependencies between them in different networks. 

All in all a little bit complicated but needed to enable the whole load scaling.

## Setup

1. Start the Application
   
     > docker compose run

2. Send basic request

    > curl http://localhost:8080/hello

3. You should see how a container is started to work on your request and if no other request accures in 10 seconds its destroyed again.


## Try some functions

To see that the whole application is scaling up and down, depending on the load we created a little script that lanuches multiple requests per secound to see the behavior.


## Create an own Function

1. Create new folder in `faasRuntime/python/functions` with an unique name

    > cd /faasRuntime/python/functions

2. Use the template defined inside of `faasRuntime/template.txt` and create an Python file inside the folder, named by your choise.
3. Define your function that should be executed
4. Go into the schema file found under `faasRuntime/global-function-definition.yml` where you need to register your function with the following parameters:
   ```
   example-faas:
      name: "example-faas-name"
      version: 0.1
      configuration:
         programing_language: "python"
         base_function_file_name: "example.py"
         function_folder: "example"
         extra_dependencies: ""
   ```
   




## Note:

- Some bugs could accure, we tested the pre defined functions and they should work hopefully :D
- An restart and rebuild could fix an error if not contact us 


Hopefully you like it :)