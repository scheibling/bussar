# Bussar
Bus departures via the swedish Trafiklab Realtime API displayed on a web page.

To show this, you need a realtime API key from Trafiklab (free for up to 100 000 requests/month) and a list of the stops you want to display. See config.example.yaml for more configuration, and below for some screenshot examples.

## Running
### Docker
You can find the docker image at ```ghcr.io/scheibling/bussar:latest```. The command to run it is:

```bash
docker run -d --name bussar \
    -p 8080:8080 \
    -v /path/to/config.yaml:/config.yaml:ro \
    ghcr.io/scheibling/bussar:latest \
    bussar -config /config.yaml
```

### Go
You can also run it directly with Go. Just clone the repo, edit the config.yaml file, and run:

```bash
go run . -config config.yaml
```

Once running, you can access the web interface at http://localhost:8080 (or the port you configured).


## Screenshots
### Flipboard (/flipper)
This board shows all departures for all stops in the config in an airport-style flipboard layout.

![bussavgångar](images/animated.gif)

### Theme 1 (/)
![alt text](images/image.png)

### Theme 2 (/)
![alt text](images/image-1.png)

### Theme 3 (/)
![alt text](images/image-3.png)

### Settings (/)
![alt text](images/image-4.png)

