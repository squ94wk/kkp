/*
Copyright 2020 The Kubermatic Kubernetes Platform contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"k8c.io/kubermatic/v2/pkg/collectors"
	"k8c.io/kubermatic/v2/pkg/log"
	"k8c.io/kubermatic/v2/pkg/resources/certificates"

	"k8s.io/client-go/tools/clientcmd"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	logOpts := log.NewDefaultOptions()
	logOpts.AddFlags(flag.CommandLine)

	endpointWithProto := flag.String("endpoint", "", "The s3 endpoint, e.G. https://my-s3.com:9000")
	accessKeyID := flag.String("access-key-id", "", "S3 Access key, defaults to the ACCESS_KEY_ID environment variable")
	secretAccessKey := flag.String("secret-access-key", "", "S3 Secret Access Key, defaults to the SECRET_ACCESS_KEY evnironment variable")
	bucket := flag.String("bucket", "kubermatic-etcd-backups", "The bucket to monitor")
	kubeconfig := flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	listenAddress := flag.String("address", ":9340", "The port to listen on")
	caBundleFile := flag.String("ca-bundle", "", "Filename of the CA bundle to use (if not given, default system certificates are used)")
	flag.Parse()

	// setup logging
	rawLog := log.New(logOpts.Debug, logOpts.Format)
	logger := rawLog.Sugar()

	if *accessKeyID == "" {
		*accessKeyID = os.Getenv("ACCESS_KEY_ID")
	}
	if *secretAccessKey == "" {
		*secretAccessKey = os.Getenv("SECRET_ACCESS_KEY")
	}

	if *endpointWithProto == "" || *accessKeyID == "" || *secretAccessKey == "" {
		logger.Fatal("All of 'endpoint', 'access-key-id' and 'secret-access-key' must be set!")
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		logger.Fatalw("Failed to load kubeconfig", zap.Error(err))
	}

	client, err := ctrlruntimeclient.New(config, ctrlruntimeclient.Options{})
	if err != nil {
		logger.Fatalw("Failed to create kube client", zap.Error(err))
	}

	secure := true
	if strings.HasPrefix(*endpointWithProto, "http://") {
		logger.Info("Disabling TLS due to http:// prefix in endpoint")
		secure = false
	}
	endpoint := strings.TrimPrefix(*endpointWithProto, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	options := &minio.Options{
		Creds:  credentials.NewStaticV4(*accessKeyID, *secretAccessKey, ""),
		Secure: secure,
	}

	if *caBundleFile != "" {
		bundle, err := certificates.NewCABundleFromFile(*caBundleFile)
		if err != nil {
			logger.Fatalw("Failed to load CA bundle", zap.Error(err))
		}

		options.Transport = &http.Transport{
			TLSClientConfig:    &tls.Config{RootCAs: bundle.CertPool()},
			DisableCompression: true,
		}
	}

	stopChannel := make(chan struct{})
	minioClient, err := minio.New(endpoint, options)
	if err != nil {
		logger.Fatalw("Failed to get S3 client", zap.Error(err))
	}

	minioClient.SetAppInfo("kubermatic-exporter", "v0.2")

	collectors.MustRegisterS3Collector(minioClient, client, *bucket, logger)

	http.Handle("/", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(*listenAddress, nil); err != nil {
			logger.Fatalw("Failed to listen", zap.Error(err))
		}
	}()

	logger.Infof("Successfully started, listening on %s", *listenAddress)
	<-stopChannel
	logger.Info("Shutting down")
}
