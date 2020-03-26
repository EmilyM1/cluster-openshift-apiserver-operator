package e2e_encryption_perf

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"

	oauthapiv1 "github.com/openshift/api/oauth/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	oauthclient "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	"github.com/openshift/cluster-openshift-apiserver-operator/pkg/operator/operatorclient"
	operatorlibrary "github.com/openshift/cluster-openshift-apiserver-operator/test/library"
	operatorencryption "github.com/openshift/cluster-openshift-apiserver-operator/test/library/encryption"
	library "github.com/openshift/library-go/test/library/encryption"
)

const (
	tokenStatsKey = "created oauthaccesstokens"
)

func TestPerfEncryptionTypeAESCBC(tt *testing.T) {
	ctx := context.TODO()
	clientSet := getPerfClients(tt)
	library.TestPerfEncryptionTypeAESCBC(tt, library.PerfScenario{
		BasicScenario: library.BasicScenario{
			Namespace:                       operatorclient.GlobalMachineSpecifiedConfigNamespace,
			LabelSelector:                   "encryption.apiserver.operator.openshift.io/component" + "=" + operatorclient.TargetNamespace,
			EncryptionConfigSecretName:      fmt.Sprintf("encryption-config-%s", operatorclient.TargetNamespace),
			EncryptionConfigSecretNamespace: operatorclient.GlobalMachineSpecifiedConfigNamespace,
			OperatorNamespace:               operatorclient.OperatorNamespace,
			TargetGRs:                       operatorencryption.DefaultTargetGRs,
			AssertFunc:                      operatorencryption.AssertRoutesAndTokens,
		},
		GetOperatorConditionsFunc: func(t testing.TB) ([]operatorv1.OperatorCondition, error) {
			apiServerOperator, err := clientSet.OperatorClient.Get(ctx, "cluster", metav1.GetOptions{})
			if err != nil {
				return nil, err
			}
			return apiServerOperator.Status.Conditions, nil
		},
		AssertDBPopulatedFunc: func(t testing.TB, errorStore map[string]int, statStore map[string]int) {
			tokenCount, ok := statStore[tokenStatsKey]
			if !ok {
				err := errors.New("missing oauth access tokens count stats, can't continue the test")
				require.NoError(t, err)
			}
			if tokenCount < 14000 {
				err := fmt.Errorf("expected to create at least 14000 tokens but %d were created", tokenCount)
				require.NoError(t, err)
			}
			t.Logf("Created %d access tokens", tokenCount)
		},
		AssertMigrationTime: func(t testing.TB, migrationTime time.Duration) {
			t.Logf("migration took %v", migrationTime)
			expectedMigrationTime := 10 * time.Minute
			if migrationTime > expectedMigrationTime {
				t.Errorf("migration took too long (%v), expected it to take no more than %v", migrationTime, expectedMigrationTime)
			}
		},
		DBLoaderWorkers: 3,
		DBLoaderFunc: library.DBLoaderRepeat(1, false,
			library.DBLoaderRepeatParallel(5010, 50, false, createAccessTokenWrapper(ctx, clientSet.TokenClient), reportSecret)),
	})
}

func createAccessTokenWrapper(ctx context.Context, tokenClient oauthclient.OAuthAccessTokensGetter) library.DBLoaderFuncType {
	return func(_ kubernetes.Interface, namespace string, errorCollector func(error), statsCollector func(string)) error {
		token := &oauthapiv1.OAuthAccessToken{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("encryption-token-%v", rand.String(18)),
			},
			RefreshToken: "I have no special talents. I am only passionately curious",
			UserName:     "kube:admin",
			Scopes:       []string{"user:full"},
			RedirectURI:  "redirect.me.to.token.of.life",
			ClientName:   "console",
			UserUID:      "non-existing-user-id",
		}
		_, err := tokenClient.OAuthAccessTokens().Create(ctx, token, metav1.CreateOptions{})
		return err
	}
}

func reportSecret(_ kubernetes.Interface, _ string, _ func(error), statsCollector func(string)) error {
	statsCollector(tokenStatsKey)
	return nil
}

func getPerfClients(t testing.TB) operatorencryption.ClientSet {
	t.Helper()

	kubeConfig, err := operatorlibrary.NewClientConfigForTest()
	require.NoError(t, err)

	kubeConfig.QPS = 300
	kubeConfig.Burst = 600

	return operatorencryption.GetClientsFor(t, kubeConfig)
}
