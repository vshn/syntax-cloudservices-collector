package sos

import (
	"context"
	"fmt"
	pipeline "github.com/ccremer/go-command-pipeline"
	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/vshn/exoscale-metrics-collector/src/kubernetes"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	"golang.org/x/exp/slices"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	organizationLabel = "appuio.io/organization"
	namespaceLabel    = "crossplane.io/claim-namespace"
)

type ObjectStorage struct {
	k8sClusters    *[]kubernetes.Cluster
	exoscaleClient *egoscale.Client
	bucketDetails  []BucketDetail
}

type BucketDetail struct {
	ClusterName  string
	Namespace    string
	Organization string
	BucketName   string
}

func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClusters *[]kubernetes.Cluster) ObjectStorage {
	return ObjectStorage{
		exoscaleClient: exoscaleClient,
		k8sClusters:    k8sClusters,
	}
}

func (o *ObjectStorage) Execute(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Running metrics collector by step")

	p := pipeline.NewPipeline[context.Context]()
	p.WithSteps(
		p.NewStep("Fetch managed buckets", o.fetchManagedBuckets),
		p.NewStep("Get bucket usage", o.getBucketUsage),
	)
	return p.RunWithContext(ctx)
}

func (o *ObjectStorage) getBucketUsage(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching bucket usage from Exoscale")
	resp, err := o.exoscaleClient.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return err
	}

	for _, bucketUsage := range *resp.JSON200.SosBucketsUsage {
		name := *bucketUsage.Name
		zoneName := *bucketUsage.ZoneName
		size := *bucketUsage.Size
		createdAt := *bucketUsage.CreatedAt

		log.V(1).Info("Trying to find a match in a cluster", "bucket name", name)
		// Match bucket name from exoscale with the bucket found in one of the clusters
		bucketIndex := slices.IndexFunc(o.bucketDetails, func(ni BucketDetail) bool { return ni.BucketName == name })

		if bucketIndex == -1 {
			log.Info("Cannot find bucket in any cluster", "bucket name", name)
			continue
		}
		fmt.Printf("name: %s, zoneName: %s, size: %d, createdAt: %s, namespace info: %s\n", name, zoneName, size, createdAt, o.bucketDetails[bucketIndex])
	}
	return nil
}

func (o *ObjectStorage) fetchManagedBuckets(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching buckets from clusters")
	for _, k8sCluster := range *o.k8sClusters {
		buckets := exoscalev1.BucketList{}
		log.V(1).Info("Listing buckets from cluster", "cluster name", k8sCluster.Name)
		err := k8sCluster.List(ctx, &buckets)
		if err != nil {
			return fmt.Errorf("cannot list buckets: %w", err)
		}
		bucketDetailsByCluster := matchOrgAndNamespaceByBucket(ctx, &buckets, k8sCluster.Name)

		// add BucketDetail from each cluster to one single slice
		o.bucketDetails = append(o.bucketDetails, *bucketDetailsByCluster...)
	}
	return nil
}

func matchOrgAndNamespaceByBucket(ctx context.Context, buckets *exoscalev1.BucketList, clusterName string) *[]BucketDetail {
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("Gathering more information from buckets", "cluster name", clusterName)

	var bucketDetails []BucketDetail
	for _, bucket := range buckets.Items {
		log.V(1).Info("Gathering more information on bucket", "bucket name", bucket.Name, "cluster name", clusterName)
		bucketDetail := BucketDetail{
			BucketName:  bucket.Spec.ForProvider.BucketName,
			ClusterName: clusterName,
		}
		if organization, exist := bucket.ObjectMeta.Labels[organizationLabel]; exist {
			bucketDetail.Organization = organization
		} else {
			// cannot get organization from bucket
			log.Info("Organization label is missing in bucket", "bucket name", bucket.Name, "label", organizationLabel)
		}
		if namespace, exist := bucket.ObjectMeta.Labels[namespaceLabel]; exist {
			bucketDetail.Namespace = namespace
		} else {
			// cannot get namespace from bucket
			log.Info("Namespace label is missing in bucket", "bucket name", bucket.Name, "label", namespaceLabel)
		}
		bucketDetails = append(bucketDetails, bucketDetail)
	}
	return &bucketDetails
}
