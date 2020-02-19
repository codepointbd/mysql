/*
Copyright The Kmodules Authors.

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

package v1alpha2

import (
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/golang/glog"
	api "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cs "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/typed/certmanager/v1alpha2"
	"github.com/pkg/errors"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	kutil "kmodules.xyz/client-go"
)

func CreateOrPatchCertificate(c cs.CertmanagerV1alpha2Interface, meta metav1.ObjectMeta, transform func(alert *api.Certificate) *api.Certificate) (*api.Certificate, kutil.VerbType, error) {
	cur, err := c.Certificates(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
	if kerr.IsNotFound(err) {
		glog.V(3).Infof("Creating Certificate %s/%s.", meta.Namespace, meta.Name)
		out, err := c.Certificates(meta.Namespace).Create(transform(&api.Certificate{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Certificate",
				APIVersion: api.SchemeGroupVersion.String(),
			},
			ObjectMeta: meta,
		}))
		return out, kutil.VerbCreated, err
	} else if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	return PatchCertificate(c, cur, transform)
}

func PatchCertificate(c cs.CertmanagerV1alpha2Interface, cur *api.Certificate, transform func(*api.Certificate) *api.Certificate) (*api.Certificate, kutil.VerbType, error) {
	return PatchCertificateObject(c, cur, transform(cur.DeepCopy()))
}

func PatchCertificateObject(c cs.CertmanagerV1alpha2Interface, cur, mod *api.Certificate) (*api.Certificate, kutil.VerbType, error) {
	curJson, err := json.Marshal(cur)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	modJson, err := json.Marshal(mod)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}

	patch, err := jsonpatch.CreateMergePatch(curJson, modJson)
	if err != nil {
		return nil, kutil.VerbUnchanged, err
	}
	if len(patch) == 0 || string(patch) == "{}" {
		return cur, kutil.VerbUnchanged, nil
	}
	glog.V(3).Infof("Patching Certificate %s/%s with %s.", cur.Namespace, cur.Name, string(patch))
	out, err := c.Certificates(cur.Namespace).Patch(cur.Name, types.MergePatchType, patch)
	return out, kutil.VerbPatched, err
}

func TryUpdateCertificate(c cs.CertmanagerV1alpha2Interface, meta metav1.ObjectMeta, transform func(*api.Certificate) *api.Certificate) (result *api.Certificate, err error) {
	attempt := 0
	err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
		attempt++
		cur, e2 := c.Certificates(meta.Namespace).Get(meta.Name, metav1.GetOptions{})
		if kerr.IsNotFound(e2) {
			return false, e2
		} else if e2 == nil {
			result, e2 = c.Certificates(cur.Namespace).Update(transform(cur.DeepCopy()))
			return e2 == nil, nil
		}
		glog.Errorf("Attempt %d failed to update Certificate %s/%s due to %v.", attempt, cur.Namespace, cur.Name, e2)
		return false, nil
	})

	if err != nil {
		err = errors.Errorf("failed to update Certificate %s/%s after %d attempts due to %v", meta.Namespace, meta.Name, attempt, err)
	}
	return
}

func UpdateCertificateStatus(
	c cs.CertmanagerV1alpha2Interface,
	in *api.Certificate,
	transform func(*api.CertificateStatus) *api.CertificateStatus,
) (result *api.Certificate, err error) {
	apply := func(x *api.Certificate) *api.Certificate {
		return &api.Certificate{
			TypeMeta:   x.TypeMeta,
			ObjectMeta: x.ObjectMeta,
			Spec:       x.Spec,
			Status:     *transform(in.Status.DeepCopy()),
		}
	}

	attempt := 0
	cur := in.DeepCopy()
	err = wait.PollImmediate(kutil.RetryInterval, kutil.RetryTimeout, func() (bool, error) {
		attempt++
		var e2 error
		result, e2 = c.Certificates(in.Namespace).UpdateStatus(apply(cur))
		if kerr.IsConflict(e2) {
			latest, e3 := c.Certificates(in.Namespace).Get(in.Name, metav1.GetOptions{})
			switch {
			case e3 == nil:
				cur = latest
				return false, nil
			case kutil.IsRequestRetryable(e3):
				return false, nil
			default:
				return false, e3
			}
		} else if err != nil && !kutil.IsRequestRetryable(e2) {
			return false, e2
		}
		return e2 == nil, nil
	})

	if err != nil {
		err = fmt.Errorf("failed to update status of Certificate %s/%s after %d attempts due to %v", in.Namespace, in.Name, attempt, err)
	}
	return
}
