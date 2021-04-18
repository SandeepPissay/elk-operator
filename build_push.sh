make build && docker build -t $1 -f dev/Dockerfile . && docker push $1
