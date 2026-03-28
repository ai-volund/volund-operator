FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/volund-operator ./cmd/operator

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/volund-operator /bin/volund-operator
ENTRYPOINT ["/bin/volund-operator"]
