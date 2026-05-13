// The locked curriculum from the plan. SessionNav and the landing page both
// read from this single source of truth. Slugs match docs/{zh,en}/<slug>.md.
//
// "available: false" means the chapter exists in the curriculum but its
// docs aren't written yet — the link will render but go to a placeholder.

export type ChapterMeta = {
  slug: string;
  num: string;
  title: { zh: string; en: string };
  available: boolean;
};

export const CURRICULUM: ChapterMeta[] = [
  { slug: "s01-min-harness",     num: "s01",    title: { zh: "最小 harness：CLI + JSON 输出",     en: "Minimum harness: CLI + JSON" },                  available: true },
  { slug: "s02-skill-md",        num: "s02",    title: { zh: "SKILL.md 解析与渲染",                en: "SKILL.md parser & renderer" },                    available: true  },
  { slug: "s03-skill-gen",       num: "s03",    title: { zh: "从 CLI 自动生成 SKILL.md",          en: "Skill generator from a CLI" },                    available: true },
  { slug: "s04-preview-bundle",  num: "s04",    title: { zh: "预览包：指纹与缓存",                 en: "Preview bundles & cache" },                       available: true  },
  { slug: "s05-repl-skin",       num: "s05",    title: { zh: "REPL 外壳：交互式 harness",          en: "REPL skin: interactive harness" },                available: true  },
  { slug: "s06-hub-registry",    num: "s06",    title: { zh: "CLI-Hub 注册中心",                   en: "CLI-Hub registry" },                              available: false },
  { slug: "s07-installer",       num: "s07",    title: { zh: "多后端安装器",                       en: "Multi-backend installer" },                       available: false },
  { slug: "s08-verify-plugin",   num: "s08",    title: { zh: "插件验证与测试桩",                   en: "Plugin verification & test stub" },               available: false },
  { slug: "s09-anygen-remote",   num: "s09",    title: { zh: "anygen：远程 API harness 案例",      en: "anygen — remote-API harness" },                   available: false },
  { slug: "s10-publish-flow",    num: "s10",    title: { zh: "发布流：CI + 注册中心同步",          en: "Publish flow: CI + registry" },                   available: false },
  { slug: "s_full-integration",  num: "s_full", title: { zh: "端到端集成穿刺",                     en: "End-to-end integration trace" },                  available: false },
  { slug: "appendix-a-agent-native-thesis", num: "A", title: { zh: "附录 A · 为何 CLI 适合 agent", en: "Appendix A · Why CLIs for agents" },              available: false },
  { slug: "appendix-b-upstream-map",         num: "B", title: { zh: "附录 B · 上游源码导读地图",   en: "Appendix B · Upstream source-reading map" },      available: false },
];

export type Locale = "zh" | "en";
export function chapterTitle(c: ChapterMeta, locale: Locale): string { return c.title[locale]; }
