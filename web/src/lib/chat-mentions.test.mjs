import { activeMention, mentionSearch, mentionToken, selectedMentions } from "./chat-mentions.ts";
import assert from "node:assert/strict";
import test from "node:test";

test("mention trigger supports Chinese and cursor placement without hijacking email", () => {
  assert.equal(activeMention("user@example.com", 16), null);
  assert.equal(activeMention("已选 @[漏洞#1 X]", 12), null);
  assert.deepEqual(activeMention("查看@漏洞 后面的文字", 5), { start: 2, end: 5, query: "漏洞" });
  assert.equal(activeMention("@漏洞\n下一行", 8), null);
});

test("categories, English and legacy Chinese aliases, IP and keyword search", () => {
  assert.equal(mentionSearch("").categories.length, 9);
  assert.equal(mentionSearch("find").categories[0].kind, "finding");
  assert.equal(mentionSearch("finding").kind, "finding");
  assert.equal(mentionSearch("root domain example.com").query, "example.com");
  assert.equal(mentionSearch("application example").kind, "app");
  assert.equal(mentionSearch("漏").categories[0].kind, "finding");
  assert.equal(mentionSearch("漏洞").kind, "finding");
  assert.equal(mentionSearch("漏洞SQL注入").query, "SQL注入");
  assert.equal(mentionSearch("ip 192.0.2.1").kind, "ip");
  assert.equal(mentionSearch("接口 GET /api").query, "GET /api");
  assert.equal(mentionSearch("acme.com").kind, "");
});

test("tokens roundtrip labels and removing one reference preserves its neighbors", () => {
  const first = mentionToken({ kind: "finding", id: 12, label: "Title[1]\nDescription" });
  const second = mentionToken({ kind: "ip", id: 13, label: "192.0.2.1" });
  assert.equal(first, "@[finding#12 Title(1) Description]");
  assert.equal(second, "@[IP#13 192.0.2.1]");
  const value = `Analyze ${first} and ${second}`;
  const selected = selectedMentions(value);
  assert.equal(selected.length, 2);
  assert.equal(selected[0].kind, "finding");
  assert.equal(selected[0].id, "12");
  assert.equal(selected[0].text, "Title(1) Description");
  assert.equal(selected[1].kind, "ip");
  const next = value.slice(0, selected[0].start) + value.slice(selected[0].start + selected[0].token.length);
  assert.equal(selectedMentions(next)[0].token, second);
});

test("all mention kinds serialize with the server's English labels", () => {
  const labels = [
    ["finding", "finding"],
    ["asset", "asset"],
    ["company", "company"],
    ["endpoint", "endpoint"],
    ["ip", "IP"],
    ["app", "application"],
    ["root_domain", "root domain"],
    ["subdomain", "subdomain"],
    ["service", "service"],
  ];
  for (const [kind, label] of labels) {
    const token = mentionToken({ kind, id: 7, label: "Example" });
    assert.equal(token, `@[${label}#7 Example]`);
    assert.equal(selectedMentions(token)[0].kind, kind);
  }
});

test("saved Chinese mention tokens remain selectable", () => {
  const selected = selectedMentions("Review @[漏洞#12 Existing finding] and @[域名#13 example.com]");
  assert.deepEqual(
    selected.map(({ kind, id }) => ({ kind, id })),
    [
      { kind: "finding", id: "12" },
      { kind: "root_domain", id: "13" },
    ],
  );
});
