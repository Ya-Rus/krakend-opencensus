package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/podliy16/krakend/config"
	"github.com/podliy16/krakend/logging"
	"github.com/podliy16/krakend/proxy"
	"github.com/podliy16/krakend/router"
	krakendgin "github.com/podliy16/krakend/router/gin"
	"github.com/podliy16/krakend/transport/http/client"

	opencensus "github.com/Ya-Rus/krakend-opencensus"
	"github.com/Ya-Rus/krakend-opencensus/exporter"
	_ "github.com/Ya-Rus/krakend-opencensus/exporter/influxdb"
	_ "github.com/Ya-Rus/krakend-opencensus/exporter/jaeger"
	_ "github.com/Ya-Rus/krakend-opencensus/exporter/prometheus"
	_ "github.com/Ya-Rus/krakend-opencensus/exporter/zipkin"
	opencensusgin "github.com/Ya-Rus/krakend-opencensus/router/gin"
)

func main() {
	port := flag.Int("p", 0, "Port of the service")
	logLevel := flag.String("l", "ERROR", "Logging level")
	debug := flag.Bool("d", false, "Enable the debug")
	configFile := flag.String("c", "/etc/krakend/configuration.json", "Path to the configuration filename")
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case sig := <-sigs:
			log.Println("Signal intercepted:", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	parser := config.NewParser()
	serviceConfig, err := parser.Parse(*configFile)
	if err != nil {
		log.Fatal("ERROR:", err.Error())
	}
	serviceConfig.Debug = serviceConfig.Debug || *debug
	if *port != 0 {
		serviceConfig.Port = *port
	}

	logger, _ := logging.NewLogger(*logLevel, os.Stdout, "[KRAKEND]")

	// Register stats and trace exporters to export the collected data.
	{
		exporter.Register(logger)

		if err := opencensus.Register(ctx, serviceConfig); err != nil {
			log.Fatal(err)
		}
	}

	bf := func(cfg *config.Backend) proxy.Proxy {
		return proxy.NewHTTPProxyWithHTTPExecutor(cfg, opencensus.HTTPRequestExecutorFromConfig(client.NewHTTPClient, cfg), cfg.Decoder)
	}

	// setup the krakend router
	routerFactory := krakendgin.NewFactory(krakendgin.Config{
		Engine:         gin.Default(),
		ProxyFactory:   opencensus.ProxyFactory(proxy.NewDefaultFactory(opencensus.BackendFactory(bf), logger)),
		Middlewares:    []gin.HandlerFunc{},
		Logger:         logger,
		HandlerFactory: opencensusgin.New(krakendgin.EndpointHandler),
		RunServer:      router.RunServer,
	})

	// start the engine
	routerFactory.NewWithContext(ctx).Run(serviceConfig)
}
