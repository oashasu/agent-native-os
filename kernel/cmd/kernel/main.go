package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/example/agent-native-microkernel/internal/authz"
	"github.com/example/agent-native-microkernel/internal/clientgateway"
	"github.com/example/agent-native-microkernel/internal/contractmeta"
	"github.com/example/agent-native-microkernel/internal/manifest"
	"github.com/example/agent-native-microkernel/internal/policy"
	"github.com/example/agent-native-microkernel/internal/registry"
	"github.com/example/agent-native-microkernel/internal/router"
	"github.com/example/agent-native-microkernel/internal/supervisor"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

type loaded struct {
	m          manifest.Manifest
	path, hash string
}

func main() {
	pluginsDir := flag.String("plugins", "./plugins", "manifest directory")
	policyPath := flag.String("policy", "./policy.json", "authorization grants")
	socket := flag.String("socket", "/tmp/vibe-kernel.sock", "external client unix socket")
	bindingsPath := flag.String("bindings", "./bindings.json", "logical service bindings")
	contractsPath := flag.String("contracts", "./contracts", "contract catalog directory")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	reg := registry.New()
	contractCatalog, err := contractmeta.Load(*contractsPath)
	if err != nil {
		log.Fatalf("contract catalog load: %v", err)
	}
	reg.SetContractCatalog(contractCatalog)
	dataRoot := os.Getenv("VIBE_DATA_ROOT")
	if dataRoot == "" {
		dataRoot = "./data"
	}
	if err := reg.SetFenceRoot(filepath.Join(dataRoot, ".vibe-fences")); err != nil {
		log.Fatalf("fence root: %v", err)
	}
	auth := authz.New()
	policyFile, err := policy.Load(*policyPath)
	if err != nil {
		log.Fatalf("policy load: %v", err)
	}
	for id, g := range policyFile.Grants {
		auth.SetGrant(id, g)
	}
	if bf, err := registry.LoadBindings(*bindingsPath); err == nil {
		for _, b := range bf.Bindings {
			reg.SetBinding(b.Capability, b.Major, registry.Binding{Service: b.Service, Authority: b.Authority, Provider: b.Provider})
		}
	} else {
		log.Printf("bindings load: %v", err)
	}
	rt := router.New(reg, auth)
	sup := supervisor.New(reg, rt)
	matches, _ := filepath.Glob(filepath.Join(*pluginsDir, "*.manifest.json"))
	var all []loaded
	for _, p := range matches {
		m, h, err := manifest.Load(p)
		if err != nil {
			log.Fatalf("load %s: %v", p, err)
		}
		if err := reg.AddManifest(m); err != nil {
			log.Fatalf("admit %s: %v", p, err)
		}
		sup.Track(m)
		all = append(all, loaded{m, p, h})
	}
	pending := append([]loaded(nil), all...)
	for len(pending) > 0 {
		progress := false
		next := make([]loaded, 0)
		for _, x := range pending {
			if missing := reg.MissingHealthyRequired(x.m); len(missing) > 0 {
				next = append(next, x)
				continue
			}
			if _, err := sup.Start(ctx, x.m, x.path, x.hash); err != nil {
				log.Printf("failed %s: %v", x.m.Plugin.ID, err)
				continue
			}
			log.Printf("started %s %s", x.m.Plugin.ID, x.m.Plugin.Version)
			progress = true
		}
		if !progress {
			for _, x := range next {
				missing := reg.MissingHealthyRequired(x.m)
				reason := fmt.Sprintf("no healthy provider for required %v", missing)
				sup.MarkBlocked(x.m, reason)
				log.Printf("blocked %s: %s", x.m.Plugin.ID, reason)
			}
			break
		}
		pending = next
	}
	gw := clientgateway.New(*socket, rt, policyFile.ClientTokens())
	if err := gw.Start(); err != nil {
		log.Fatal(err)
	}
	defer gw.Close()
	log.Printf("client gateway listening %s", *socket)
	<-ctx.Done()
}
