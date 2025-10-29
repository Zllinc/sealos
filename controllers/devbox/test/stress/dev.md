# tester：
* 不同tester把控不同测试的流程
* tester里可以实现一些和其他tester里不一样的方法

# common tester：
* 实现与devbox/容器/镜像等需要用到k8s client的方法，由其他tester导入common结构体去调用
* 

# 设计原则：
* 相同的方法一定要提取到common tester里
* 每个tester的结构要一致：
    * 入口处先展示所有流程
    * 再加入并发的逻辑
    * 详细参考tester.go@RunCommitTest
* 每个测试的过程输出要易懂


# 目前需要提取的方法
* 写入pod数据
* 将每个测试都提取成tester