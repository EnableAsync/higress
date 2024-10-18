HEADER := Content-Type: application/json
WEAVIATE_PORT = 8081


.PHONY: proxy cache docker weaviate-post-collection weaviate-post-obj weaviate-get-obj

all: proxy cache docker weaviate-post-collection weaviate-post-obj weaviate-get-obj

docker:
	docker compose up

docker-down:
	docker compose down

# 编译 proxy 插件
proxy:
	cd ../plugins/wasm-go/extensions/ai-proxy && \
	tinygo build -o ai-proxy.wasm -scheduler=none -target=wasi -gc=custom -tags='custommalloc nottinygc_finalizer proxy_wasm_version_0_2_100' . && \
	mv ai-proxy.wasm ../../../../external/

# 编译 cache 插件
cache:
	cd ../plugins/wasm-go/extensions/ai-cache && \
	tinygo build -o ai-cache.wasm -scheduler=none -target=wasi -gc=custom -tags='custommalloc nottinygc_finalizer proxy_wasm_version_0_2_100' . && \
	mv ai-cache.wasm ../../../../external/

# 创建 object
weaviate-post-obj:
	curl --request POST --url http://localhost:$(WEAVIATE_PORT)/v1/objects -H "$(HEADER)" --data '{"class": "Higress", "vector": [0.1, 0.2, 0.3], "properties": {"question": "这里是问题3"}}'

# 创建 schema
weaviate-post-schema:
	curl -X POST "http://localhost:$(WEAVIATE_PORT)/v1/schema" -H "$(HEADER)" -d '{"class": "Higress"}'

# 获取 schema
weaviate-get-schema:
	curl -X GET "http://localhost:$(WEAVIATE_PORT)/v1/schema" -H "$(HEADER)"

# 获取 objs
weaviate-get-obj:
	curl -X GET "http://localhost:$(WEAVIATE_PORT)/v1/objects"

# 获取具体 obj
weaviate-get-obj-id:
	curl -X GET "http://localhost:$(WEAVIATE_PORT)/v1/objects/Higress/8e7df58e-3415-4264-9bcb-afbb3c51318b"

# 删除 obj
weaviate-delete-obj:
	curl -X DELETE "http://localhost:$(WEAVIATE_PORT)/v1/objects/Higress/8e7df58e-3415-4264-9bcb-afbb3c51318b"

# 删除 collection，这里 classname 会自动大写
weaviate-delete-collection:
	curl -X DELETE "http://localhost:$(WEAVIATE_PORT)/v1/schema/Higress"

QUERY = "{ \
  Get { \
    Higress ( \
      limit: 5 \
      nearVector: { \
        vector: [0.1, 0.2, 0.3] \
      } \
    ) { \
	  question \
      _additional { \
        distance \
      } \
    } \
  } \
}"
# 搜索，默认按照 distance 升序
# https://weaviate.io/developers/weaviate/config-refs/distances
weaviate-search:
	curl -X POST "http://localhost:$(WEAVIATE_PORT)/v1/graphql" -H "$(HEADER)" -d '{"query": ${QUERY}}'


es-bash:
	docker exec -it external-es-1 /bin/bash

es-post-index:
	curl -u elastic:123456 -X PUT "localhost:9200/higress?pretty"

# redis client
redis-cli:
	docker run -it --network external_wasmtest redis redis-cli -h external-redis-1

# redis flushall
redis-flushall:
	docker run -it --network external_wasmtest redis redis-cli -h external-redis-1 flushall

# llm request
llm:
	curl -X POST http://localhost:10000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "glm-4-plus", "messages": [{"role": "user", "content": "你好"}]}'

llm1:
	curl -X POST http://localhost:10000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "glm-4-plus", "messages": [{"role": "user", "content": "今天晚上吃什么"}]}'

llm2:
	curl -X POST http://localhost:10000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "glm-4-plus", "messages": [{"role": "user", "content": "今天晚上吃什么？"}]}'

llm3:
	curl -X POST http://localhost:10000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "glm-4-plus", "messages": [{"role": "user", "content": "今天晚上吃什么呢？有无推荐？"}]}'

llm4:
	curl -X POST http://localhost:10000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "glm-4-plus", "messages": [{"role": "user", "content": "今天晚上吃什么呢？有无推荐啊？"}]}'

test-llm:
	curl --location 'https://open.bigmodel.cn/api/paas/v4/chat/completions' --header 'Authorization: Bearer 67e93d524df46fca3640df67a7461c04.qOksqKAoWHcv03aV' --header 'Content-Type: application/json' --data '{"model": "glm-4-plus","messages": [{"role": "user","content": "你好"}]}'