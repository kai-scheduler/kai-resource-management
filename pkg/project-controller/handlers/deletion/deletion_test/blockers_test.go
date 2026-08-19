package deletion_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/kai-scheduler/kai-resource-management/pkg/project-controller/handlers/deletion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("BlockerGroupsFromConfigMapData", func() {
	It("returns no groups for empty data", func() {
		groups, err := BlockerGroupsFromConfigMapData(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(BeEmpty())
	})

	It("groups blockers sharing a displayName into one group", func() {
		// A flat list where two Blockers share the "Workloads" displayName and one is "Secrets".
		// They must collapse into two groups: Workloads (2 queries) and Secrets (1 query).
		// Group order is not significant, so look groups up by displayName.
		blockers := []Blocker{
			{DisplayName: "Workloads", Group: "run.ai", Version: "v2alpha1", Kind: "TrainingWorkload"},
			{DisplayName: "Workloads", Group: "run.ai", Version: "v2alpha1", Kind: "InferenceWorkload"},
			{DisplayName: "Secrets", Group: "", Version: "v1", Kind: "Secret", LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "run.ai/department", Operator: metav1.LabelSelectorOpDoesNotExist},
					{Key: "run.ai/resource", Operator: metav1.LabelSelectorOpIn, Values: []string{"password"}},
				},
			}},
		}

		encoded, err := yaml.Marshal(blockers)
		Expect(err).NotTo(HaveOccurred())
		data := map[string]string{"blockers.yaml": string(encoded)}

		groups, err := BlockerGroupsFromConfigMapData(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(HaveLen(2))

		byName := map[string]BlockerGroup{}
		for _, g := range groups {
			byName[g.DisplayName] = g
		}

		Expect(byName).To(HaveKey("Workloads"))
		Expect(byName["Workloads"].ConditionType()).To(Equal("WorkloadsReady"))
		Expect(byName["Workloads"].Reason()).To(Equal("WorkloadsDeletionHandlerFailed"))
		Expect(byName["Workloads"].Blockers).To(HaveLen(2))

		Expect(byName).To(HaveKey("Secrets"))
		Expect(byName["Secrets"].Blockers).To(HaveLen(1))
	})

	It("rejects a blocker with no displayName", func() {
		data := map[string]string{"blockers.yaml": "- version: v1\n  kind: Secret\n"}
		_, err := BlockerGroupsFromConfigMapData(data)
		Expect(err).To(HaveOccurred())
	})

	It("rejects a blocker with no kind", func() {
		data := map[string]string{"blockers.yaml": "- displayName: Secrets\n  version: v1\n"}
		_, err := BlockerGroupsFromConfigMapData(data)
		Expect(err).To(HaveOccurred())
	})
})
