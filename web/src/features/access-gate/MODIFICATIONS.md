# web/src/features/access-gate MODIFICATIONS

用途：部署访问密码门。配置接口失败时仍保留密码输入，避免后端未就绪时页面只剩错误文案、没有入口。

## 文件
- `access-gate.tsx`：加载判断用 `status === 'pending'`，不再用 `isPending`。配置读取失败时错误提示与密码框、进入按钮同时显示；重试仍走 `config.refetch()`。

## 验证
`pnpm --dir web test` 中 `access-gate.test.tsx`。
