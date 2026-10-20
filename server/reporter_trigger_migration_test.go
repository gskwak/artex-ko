package server

import "testing"

// Historical messages are escaped so source files contain no obsolete Chinese prose.
func TestHistoricalReporterToolCallMessages(t *testing.T) {
	for _, message := range []string{
		"\u4e0a\u9762\u521a\u6709\u4e00\u4e2a\u6f0f\u6d1e\u88ab report_finding \u767b\u8bb0\u3002\u8bf7\u4ece\u89e6\u53d1\u4e0a\u4e0b\u6587\u91cc\u53d6\u51fa finding_id\uff08\u5de5\u5177\u8fd4\u56de \"finding recorded: <id>\" \u91cc\u7684\u6570\u5b57\uff09\u4e0e\u4efb\u52a1 id\uff0c\u6309\u4f60\u7684\u804c\u8d23\u64b0\u5199\u8be5\u6f0f\u6d1e\u7684\u8be6\u7ec6\u62a5\u544a\uff0c\u6700\u540e\u8c03\u7528 update_finding_report(finding_id, report) \u4fdd\u5b58\u3002",
		"\u4e0a\u9762\u521a\u6709\u4e00\u4e2a\u6f0f\u6d1e\u88ab report_finding \u767b\u8bb0\u3002\u8bf7\u8bfb\u53d6\u8fd4\u56de JSON \u7684 finding_id\uff08\u72ec\u7acb\u6f0f\u6d1e\u8bb0\u5f55 ID\uff09\u4e0e finding_node_id\uff08\u63a2\u7d22\u8282\u70b9 ID\uff09\uff0c\u5148\u7528 get_finding_traffic(finding_id) \u8bfb\u53d6\u5f53\u524d\u8bc1\u636e\u6e05\u5355\u53ca\u5176 version\uff08\u7a7a\u6e05\u5355\u662f\u6b63\u5e38\u60c5\u51b5\uff0c\u7167\u5e38\u5199\u62a5\u544a\uff09\uff1b\u82e5\u8fd0\u884c\u6307\u5f15\u542f\u7528\u81ea\u52a8\u7ed1\u5b9a\uff0c\u5728\u8bfb\u53d6\u524d\u5148\u6838\u5b9e\u5e76\u5173\u8054\u672c\u6b21\u6f0f\u6d1e\u7684\u6d41\u91cf\u3002\u8282\u70b9\u8be6\u60c5\u4f7f\u7528 finding_node_id\u3002\u6700\u540e\u8c03\u7528 update_finding_report(finding_id=finding_node_id, report, evidence_version=\u5b9e\u9645\u8bfb\u53d6\u7248\u672c) \u4fdd\u5b58\uff0cevidence_version \u5fc5\u987b\u4f20\uff0c\u5426\u5219\u62a5\u544a\u4f1a\u88ab\u6c38\u4e45\u6807\u8bb0\u4e3a\u5f85\u66f4\u65b0\u3002\u4e0d\u8981\u6df7\u7528\u4e24\u79cd\u7f16\u53f7\u3002",
	} {
		if !isHistoricalReporterToolCallMessage(message) {
			t.Fatal("unchanged historical reporter trigger was not recognized")
		}
		if isHistoricalReporterToolCallMessage(message + " user edit") {
			t.Fatal("edited reporter trigger was treated as a stock default")
		}
	}
	if isHistoricalReporterToolCallMessage(reporterToolCallMessage) {
		t.Fatal("current English reporter trigger was treated as a historical default")
	}
}
