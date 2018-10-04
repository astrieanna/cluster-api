/*
Copyright 2018 The Kubernetes Authors.

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

package writer

import (
	"errors"
	"io"
	"os"

	"github.com/ghodss/yaml"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/generator"
)

// secretCertWriter provisions the certificate by reading and writing to the k8s secrets.
type secretCertWriter struct {
	*SecretCertWriterOptions

	// dnsName is the DNS name that the certificate is for.
	dnsName string
	// dryrun indicates sending the create/update request to the server or output to the writer in yaml format.
	dryrun bool
}

// SecretCertWriterOptions is options for constructing a secretCertWriter.
type SecretCertWriterOptions struct {
	// client talks to a kubernetes cluster for creating the secret.
	Client client.Client
	// certGenerator generates the certificates.
	CertGenerator generator.CertGenerator
	// secret points the secret that contains certificates that written by the CertWriter.
	Secret *types.NamespacedName
	// Writer is used in dryrun mode for writing the objects in yaml format.
	Writer io.Writer
}

var _ CertWriter = &secretCertWriter{}

func (ops *SecretCertWriterOptions) setDefaults() {
	if ops.CertGenerator == nil {
		ops.CertGenerator = &generator.SelfSignedCertGenerator{}
	}
	if ops.Writer == nil {
		ops.Writer = os.Stdout
	}
}

func (ops *SecretCertWriterOptions) validate() error {
	if ops.Client == nil {
		return errors.New("client must be set in SecretCertWriterOptions")
	}
	if ops.Secret == nil {
		return errors.New("secret must be set in SecretCertWriterOptions")
	}
	return nil
}

// NewSecretCertWriter constructs a CertWriter that persists the certificate in a k8s secret.
func NewSecretCertWriter(ops SecretCertWriterOptions) (CertWriter, error) {
	ops.setDefaults()
	err := ops.validate()
	if err != nil {
		return nil, err
	}
	return &secretCertWriter{
		SecretCertWriterOptions: &ops,
	}, nil
}

// EnsureCert provisions certificates for a webhookClientConfig by writing the certificates to a k8s secret.
func (s *secretCertWriter) EnsureCert(dnsName string, dryrun bool) (*generator.Artifacts, bool, error) {
	// Create or refresh the certs based on clientConfig
	s.dryrun = dryrun
	s.dnsName = dnsName
	return handleCommon(s.dnsName, s)
}

var _ certReadWriter = &secretCertWriter{}

func (s *secretCertWriter) buildSecret() (*corev1.Secret, *generator.Artifacts, error) {
	certs, err := s.CertGenerator.Generate(s.dnsName)
	if err != nil {
		return nil, nil, err
	}
	secret := certsToSecret(certs, *s.Secret)
	return secret, certs, err
}

func (s *secretCertWriter) write() (*generator.Artifacts, error) {
	secret, certs, err := s.buildSecret()
	if err != nil {
		return nil, err
	}
	if s.dryrun {
		return certs, s.dryrunWrite(secret)
	}
	err = s.Client.Create(nil, secret)
	if apierrors.IsAlreadyExists(err) {
		return nil, alreadyExistError{err}
	}
	return certs, err
}

func (s *secretCertWriter) overwrite() (
	*generator.Artifacts, error) {
	secret, certs, err := s.buildSecret()
	if err != nil {
		return nil, err
	}
	if s.dryrun {
		return certs, s.dryrunWrite(secret)
	}
	err = s.Client.Update(nil, secret)
	return certs, err
}

func (s *secretCertWriter) dryrunWrite(secret *corev1.Secret) error {
	sec, err := yaml.Marshal(secret)
	if err != nil {
		return err
	}
	_, err = s.Writer.Write(sec)
	return err
}

func (s *secretCertWriter) read() (*generator.Artifacts, error) {
	if s.dryrun {
		return nil, notFoundError{}
	}
	secret := &corev1.Secret{}
	err := s.Client.Get(nil, *s.Secret, secret)
	if apierrors.IsNotFound(err) {
		return nil, notFoundError{err}
	}
	return secretToCerts(secret), err
}

func secretToCerts(secret *corev1.Secret) *generator.Artifacts {
	if secret.Data == nil {
		return nil
	}
	return &generator.Artifacts{
		CACert: secret.Data[CACertName],
		Cert:   secret.Data[ServerCertName],
		Key:    secret.Data[ServerKeyName],
	}
}

func certsToSecret(certs *generator.Artifacts, sec types.NamespacedName) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: sec.Namespace,
			Name:      sec.Name,
		},
		Data: map[string][]byte{
			CACertName:     certs.CACert,
			ServerKeyName:  certs.Key,
			ServerCertName: certs.Cert,
		},
	}
}

// Inject sets the ownerReference in the secret.
func (s *secretCertWriter) Inject(objs ...runtime.Object) error {
	// TODO: figure out how to get the UID
	//for i := range objs {
	//	accessor, err := meta.Accessor(objs[i])
	//	if err != nil {
	//		return err
	//	}
	//	err = controllerutil.SetControllerReference(accessor, s.sec, scheme.Scheme)
	//	if err != nil {
	//		return err
	//	}
	//}
	//return s.client.Update(context.Background(), s.sec)
	return nil
}
