# Official Go image with the complete toolchain required by the evaluator.
FROM golang:1.25

WORKDIR /app

COPY ["go.mod","go.sum","/app/"]
RUN cd /app && GOWORK=off GOTOOLCHAIN=local go mod download

# Keep the complete project, including any project-owned Dockerfile and BENZHI_README.md.
COPY . .

RUN cd /app && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go build ./...

CMD ["bash"]
