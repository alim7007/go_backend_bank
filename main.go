package main

import (
	"context"
	"os"

	"net"
	"net/http"

	// "github.com/alim7007/go_bank_k8s/api"
	db "github.com/alim7007/go_bank_k8s/db/sqlc"
	_ "github.com/alim7007/go_bank_k8s/doc/statik"
	"github.com/alim7007/go_bank_k8s/mail"
	"github.com/alim7007/go_bank_k8s/worker"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/hibiken/asynq"

	"github.com/alim7007/go_bank_k8s/gapi"
	"github.com/alim7007/go_bank_k8s/pb"
	"github.com/alim7007/go_bank_k8s/util"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	_ "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rakyll/statik/fs"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	config, err := util.LoadConfig(".")
	if err != nil {
		log.Fatal().Err(err).Msg("cannot load config")
	}

	if config.Environment == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	conn, err := pgxpool.New(context.Background(), config.DBSource)

	if err != nil {
		log.Fatal().Err(err).Msg("cannot connect to db")
	}
	runDBMigration(config.MigrationUrl, config.DBSource)

	store := db.NewStore(conn)

	// redis and worker
	redisOpt := asynq.RedisClientOpt{
		Addr: config.Redis_Address,
	}
	taskDistributor := worker.NewRedisTaskDistributor(redisOpt)

	// run
	go runTaskProcessor(config, redisOpt, store)
	go runGrpcServer(config, store, taskDistributor)
	runGatewayServer(config, store, taskDistributor)
	// runGinServier(config, store)
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////
func runDBMigration(migrationURL string, dbSource string) {
	migration, err := migrate.New(migrationURL, dbSource)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create new migrate instance:")
	}

	if err = migration.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatal().Err(err).Msg("failed to run migrate up")
	}

	log.Info().Msg("db migrated successfully")
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////
func runGrpcServer(config util.Config, store db.Store, taskDistributor worker.TaskDistributor) {
	server, err := gapi.NewServer(config, store, taskDistributor)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create server")
	}

	grpcLogger := grpc.UnaryInterceptor(gapi.GrpcLogger)
	grpcServer := grpc.NewServer(grpcLogger)
	pb.RegisterOlimBankServer(grpcServer, server)
	reflection.Register(grpcServer)

	listener, err := net.Listen("tcp", config.GRPCServerAddress)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create listener:")
	}
	log.Info().Msgf("start grpc at %s", listener.Addr().String())
	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot start grpc server:")
	}
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////
func runGatewayServer(config util.Config, store db.Store, taskDistributor worker.TaskDistributor) {
	server, err := gapi.NewServer(config, store, taskDistributor)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create server:")
	}

	jsonOption := runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames: true,
		},
		UnmarshalOptions: protojson.UnmarshalOptions{
			DiscardUnknown: true,
		},
	})

	grpcMux := runtime.NewServeMux(jsonOption)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = pb.RegisterOlimBankHandlerServer(ctx, grpcMux, server)
	if err != nil {
		log.Fatal().Err(err).Msg("cannont register handler server")

	}

	mux := http.NewServeMux()
	mux.Handle("/", grpcMux)

	statikFS, err := fs.New()
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create statik fs:")

	}

	swaggerHandler := http.StripPrefix("/swagger/", http.FileServer(statikFS))
	mux.Handle("/swagger/", swaggerHandler)

	listener, err := net.Listen("tcp", config.GATEAWAY_HTTPServerAddress)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create listener:")

	}
	log.Info().Msgf("start http gateway server at %s", listener.Addr().String())
	handler := gapi.HttpLogger(mux)
	err = http.Serve(listener, handler)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot start http gateway server:")
	}
}

// ////////////////////////////////////////////////////////////////////////////////////////////////////
// func runGinServier(config util.Config, store db.Store) {
// 	server, err := api.NewServer(config, store)
// 	if err != nil {
// 		log.Fatal().Err(err).Msg("cannot create server:")
// 	}

//		err = server.Start(config.HTTPServerAddress)
//		if err != nil {
//			log.Fatal().Err(err).Msg("cannot start server:")
//		}
//	}
//
// ////////////////////////////////////////////////////////////////////////////////////////////////////
func runTaskProcessor(config util.Config, redisClientOpt asynq.RedisClientOpt, store db.Store) {
	mailer := mail.NewGmailSender(config.EmailSenderName, config.EmailSenderAddress, config.EmailSenderPassword)
	taskProcessor := worker.NewRedisTaskProcessor(redisClientOpt, store, mailer)
	log.Info().Msg("start task processor")
	err := taskProcessor.Start()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start task processor")
	}
}
