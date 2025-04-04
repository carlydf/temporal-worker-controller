// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package clientpool

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"sync"

	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

type ClientPoolKey struct {
	HostPort  string
	Namespace string
}

type ClientPool struct {
	mux       sync.RWMutex
	logger    log.Logger
	clients   map[ClientPoolKey]sdkclient.Client
	k8sClient runtimeclient.Client
}

func New(l log.Logger, c runtimeclient.Client) *ClientPool {
	return &ClientPool{
		logger:    l,
		clients:   make(map[ClientPoolKey]sdkclient.Client),
		k8sClient: c,
	}
}

func (cp *ClientPool) GetSDKClient(key ClientPoolKey) (sdkclient.Client, bool) {
	cp.mux.RLock()
	defer cp.mux.RUnlock()

	c, ok := cp.clients[key]
	if ok {
		return c, true
	}
	return nil, false
}

type NewClientOptions struct {
	TemporalNamespace string
	K8sNamespace      string
	Spec              v1alpha1.TemporalConnectionSpec
}

func (cp *ClientPool) UpsertClient(ctx context.Context, opts NewClientOptions) (sdkclient.Client, error) {
	clientOpts := sdkclient.Options{
		Logger:    cp.logger,
		HostPort:  opts.Spec.HostPort,
		Namespace: opts.TemporalNamespace,
		// TODO(jlegrone): fix this
		Credentials: sdkclient.NewAPIKeyStaticCredentials(os.Getenv("TEMPORAL_CLOUD_API_KEY")),
		//Credentials: client.NewAPIKeyDynamicCredentials(func(ctx context.Context) (string, error) {
		//	token, ok := os.LookupEnv("TEMPORAL_CLOUD_API_KEY")
		//	if ok {
		//		if token == "" {
		//			return "", fmt.Errorf("empty token")
		//		}
		//		return token, nil
		//	}
		//	return "", fmt.Errorf("token not found")
		//}),
		//Credentials: client.NewMTLSCredentials(tls.Certificate{
		//	Certificate:                  cert.Certificate,
		//	PrivateKey:                   cert.PrivateKey,
		//	SupportedSignatureAlgorithms: nil,
		//	OCSPStaple:                   nil,
		//	SignedCertificateTimestamps:  nil,
		//	Leaf:                         nil,
		//}),
	}
	// Get the connection secret if it exists
	if opts.Spec.MutualTLSSecret != "" {
		var secret corev1.Secret
		if err := cp.k8sClient.Get(ctx, types.NamespacedName{
			Name:      opts.Spec.MutualTLSSecret,
			Namespace: opts.K8sNamespace,
		}, &secret); err != nil {
			return nil, err
		}
		if secret.Type != corev1.SecretTypeTLS {
			err := fmt.Errorf("secret %s must be of type kubernetes.io/tls", secret.Name)
			return nil, err
		}
		cert, err := tls.X509KeyPair(secret.Data["tls.crt"], secret.Data["tls.key"])
		if err != nil {
			return nil, err
		}
		clientOpts.ConnectionOptions.TLS = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
	}

	c, err := sdkclient.Dial(clientOpts)
	if err != nil {
		return nil, err
	}

	if _, err := c.CheckHealth(context.Background(), &sdkclient.CheckHealthRequest{}); err != nil {
		panic(err)
	}

	cp.mux.Lock()
	defer cp.mux.Unlock()

	key := ClientPoolKey{
		HostPort:  opts.Spec.HostPort,
		Namespace: opts.TemporalNamespace,
	}
	cp.clients[key] = c

	return c, nil
}

func (cp *ClientPool) Close() {
	cp.mux.Lock()
	defer cp.mux.Unlock()

	for _, c := range cp.clients {
		c.Close()
	}

	cp.clients = make(map[ClientPoolKey]sdkclient.Client)
}
