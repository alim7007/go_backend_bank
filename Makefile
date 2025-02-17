DB_URL=postgresql://root:olim123@localhost:5432/olimbank?sslmode=disable

postgres:
	docker run --name postgresdb_c --network bank_network -p 5432:5432 -e POSTGRES_USER=root -e \
	POSTGRES_PASSWORD=olim123 -v postgresdb_c:/var/lib/postgresql/data -d postgres 
run_dockerfile:
	docker run --name olimbank --network bank_network -p 8080:8080 -e \
	DB_SOURCE="postgresql://root:olim123@postgresdb_c:5432/olimbank?sslmode=disable" -e REDIS_ADDRESS="redis_c:6379" olimbank:1.1
createdb:
	docker exec -it postgresdb_c createdb --username=root --owner=root olimbank
dropdb:
	docker exec -it postgresdb_c dropdb olimbank
new_migrate:
	migrate create -ext sql -dir db/migration -seq $(name)
migrateup:
	migrate -path db/migration -database "$(DB_URL)" -verbose up
migrateup1:
	migrate -path db/migration -database "$(DB_URL)" -verbose up 1
migratedown:
	migrate -path db/migration -database "$(DB_URL)" -verbose down
migratedown1:
	migrate -path db/migration -database "$(DB_URL)" -verbose down 1
sqlc:
	sqlc generate
test:
	go test -v -cover -short ./...
mock:
	mockgen -package mockdb -destination db/mock/store.go github.com/alim7007/go_bank_k8s/db/sqlc Store
db_docs:
	dbdocs build doc/db.dbml

db_schema:
	dbml2sql --postgres -o doc/schema.sql doc/db.dbml
proto:
	rm -f pb/*.go
	rm -f doc/swagger/*swagger.json
	protoc --proto_path=proto --go_out=pb --go_opt=paths=source_relative \
    --go-grpc_out=pb --go-grpc_opt=paths=source_relative \
	--grpc-gateway_out=pb --grpc-gateway_opt=paths=source_relative \
	--openapiv2_out=doc/swagger --openapiv2_opt=allow_merge=true,merge_file_name=olimbank \
	proto/*.proto
	statik -src=./doc/swagger -dest=./doc
	
evans:
	evans --host localhost --port 9090 -r repl
redis: 
	docker run --name redis_c -p 6379:6379 --network bank_network -d redis:7-alpine
check_redis:
	docker exec -it redis_c redis-cli ping

.PHONY: postgres createdb dropdb migrateup migrateup1 migratedown migratedown1 sqlc test mock run_dockerfile db_docs db_schema proto evans redis check_redis new_migrate