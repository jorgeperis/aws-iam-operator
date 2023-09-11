package controllers

import (
	"context"
	"time"

	awssdk "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	awsiam "github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
	"github.com/go-logr/logr"
	"github.com/redradrat/cloud-objects/aws/iam"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redradrat/cloud-objects/aws"

	iamv1beta1 "github.com/redradrat/aws-iam-operator/api/v1beta1"
)

type AWSObjectStatusResource interface {
	GetStatus() *iamv1beta1.AWSObjectStatus
	RuntimeObject() client.Object
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func CreateAWSObject(svc iamiface.IAMAPI, ins aws.Instance, preFunc func() error) (StatusUpdater, error) {

	if err := preFunc(); err != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	if err := ins.Create(svc); err != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	return SuccessStatusUpdater(), nil
}

func UpdateAWSObject(svc iamiface.IAMAPI, ins aws.Instance, preFunc func() error) (StatusUpdater, error) {

	if err := preFunc(); err != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	if err := ins.Update(svc); err != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	return SuccessStatusUpdater(), nil
}

func DeleteAWSObject(svc iamiface.IAMAPI, ins aws.Instance, preFunc func() error) (StatusUpdater, error) {

	if err := preFunc(); err != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	if err := ins.Delete(svc); ignoreDoesNotExistError(err) != nil {
		return ErrorStatusUpdater(err.Error()), err
	}

	return DoNothingStatusUpdater, nil
}

func ignoreDoesNotExistError(err error) error {
	if err != nil {
		if castErr, ok := err.(aws.InstanceError); ok {
			if castErr.IsOfErrorCode(aws.ErrAWSInstanceNotYetCreated) {
				return nil
			}
		}
	}
	return err
}

func DoNothingPreFunc() error { return nil }

func errWithStatus(obj AWSObjectStatusResource, err error, sw client.StatusWriter, ctx context.Context) error {
	origerr := err
	obj.GetStatus().Message = origerr.Error()
	obj.GetStatus().State = iamv1beta1.ErrorSyncState
	if err = sw.Update(ctx, obj.RuntimeObject()); err != nil {
		return err
	}
	return origerr
}

func IAMService(region string) (*awsiam.IAM, error) {
	session, err := session.NewSession(&awssdk.Config{
		Region: awssdk.String(region)},
	)
	if err != nil {
		return nil, err
	}

	return iam.Client(session), nil
}

type StatusUpdater func(ins aws.Instance, obj AWSObjectStatusResource, ctx context.Context, sw client.StatusWriter, log logr.Logger)

func SuccessStatusUpdater() StatusUpdater {
	return func(ins aws.Instance, obj AWSObjectStatusResource, ctx context.Context, sw client.StatusWriter, log logr.Logger) {
		obj.GetStatus().ARN = ins.ARN().String()
		obj.GetStatus().Message = "Succesfully reconciled"
		obj.GetStatus().State = iamv1beta1.OkSyncState
		obj.GetStatus().LastSyncAttempt = time.Now().Format(time.RFC822Z)

		err := sw.Update(ctx, obj.RuntimeObject())
		if err != nil {
			log.Error(err, "unable to write status to resource")
		}
	}
}

func ErrorStatusUpdater(reason string) StatusUpdater {
	return func(ins aws.Instance, obj AWSObjectStatusResource, ctx context.Context, sw client.StatusWriter, log logr.Logger) {
		obj.GetStatus().Message = reason
		obj.GetStatus().State = iamv1beta1.ErrorSyncState
		obj.GetStatus().LastSyncAttempt = time.Now().Format(time.RFC822Z)

		err := sw.Update(ctx, obj.RuntimeObject())
		if err != nil {
			log.Error(err, "unable to write status to resource")
		}
	}
}

func DoNothingStatusUpdater(ins aws.Instance, obj AWSObjectStatusResource, ctx context.Context, sw client.StatusWriter, log logr.Logger) {
}

func DeletePolicyVersion(svc iamiface.IAMAPI, policyARN string, versionID string) error {
	_, err := svc.DeletePolicyVersion(&awsiam.DeletePolicyVersionInput{
		PolicyArn: &policyARN,
		VersionId: &versionID,
	})

	return err
}

func CleanUpPolicyVersions(svc iamiface.IAMAPI, policyARN string) error {
	maxVersions := 4
	resp, err := svc.ListPolicyVersions(&awsiam.ListPolicyVersionsInput{
		PolicyArn: &policyARN,
	})

	if err != nil {
		return err
	}

	if len(resp.Versions) <= maxVersions {
		return nil
	}

	// We need to delete oldest versions
	for i := len(resp.Versions) - 1; i >= maxVersions; i-- {
		err := DeletePolicyVersion(svc, policyARN, *resp.Versions[i].VersionId)
		if err != nil {
			return err
		}
	}

	return nil
}
