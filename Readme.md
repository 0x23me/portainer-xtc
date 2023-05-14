# portainer-xtc

## Summary

This program serves as a utility to manage Docker stacks on Portainer. It reads Docker Compose files from a directory and deploys or updates them on the corresponding Portainer node. The directories are structured as `node/stack/docker-compose.yml`. The program supports real-time updates by watching the stack directory and redeploying on changes.

## Usage

The program uses the following command line flags:

- `--portainer-address`: The address of the Portainer API.
- `--api-key`: The API key for Portainer.
- `--stack-files-dir`: The directory where the stack files are located (default: `./stacks/`).
- `--watch-stacks-dir` (`-w`): If set, the program will watch the stack directory and update stacks on changes (default: `false`).

You can run the program with these flags like so:

```bash
./portainer-xtc --portainer-address http://portainer.example.com --api-key my-api-key --stack-files-dir /path/to/stacks --watch-stacks-dir
```

## Building the Binary

To build the binary for this program, you need to have Go installed on your machine. Once you have Go installed, you can build the binary by running the following command in the root directory of the project:

```bash
make build
```

This will produce a binary named `portainer-xtc` in the current directory.

## Building the Docker Image

To build a Docker image for this program, you need to have Docker installed on your machine. Once you have Docker installed, you can build the image by running the following command in the root directory of the project:

```bash
docker build -t myusername/portainer-xtc .
```

Replace `myusername` with your Docker Hub username. This will produce a Docker image named `myusername/portainer-xtc`.

You can then run the Docker image with the following command:

```bash
docker run -v /path/to/stacks:/stacks myusername/portainer-xtc --portainer-address http://portainer.example.com --api-key my-api-key --stack-files-dir /stacks --watch-stacks-dir
```

Replace `/path/to/stacks` with the path to your stack files. This command will run the `portainer-xtc` Docker container and map the local directory `/path/to/stacks` to the `/stacks` directory in the container.
