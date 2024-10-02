# Architecture

A golang server which takes in a file, divides it into 3chunks and writes down the chunks to file system.
The file IDs are saved in postgres as metadata.
The system is scalable as the files are written to disk in parallel.

# Running Instructions

## Start Program

```sh
docker compose up --build -d
docker ps # check everything is running successfully
```

## Run Migrations

```sh
go run cmd/main.go
```

## Upload file

```sh
curl -X POST -H "Content-Type: multipart/form-data" -F "file=@./testfile.txt" http://localhost:8080/upload

# note, you can upload multiple files with correct paths
```

## Query file

```sh
curl -X GET http://localhost:8080/files | jq
```

## Rename/Remove original file

```sh
rm testfile.txt
```

## Dowbload file

```sh
# use correct file id - http://localhost:8080/download/{fileId}, fetched after querying for the files
curl -X GET -O -J http://localhost:8080/download/1
cat testfile.txt
```