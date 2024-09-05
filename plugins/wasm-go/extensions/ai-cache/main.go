// 这个文件中主要将OnHttpRequestHeaders、OnHttpRequestBody、OnHttpResponseHeaders、OnHttpResponseBody这四个函数实现
// 其中的缓存思路调用cache.go中的逻辑，然后cache.go中的逻辑会调用textEmbeddingProvider和vectorStoreProvider中的逻辑（实例）
package main

import (
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-cache/config"
	"github.com/alibaba/higress/plugins/wasm-go/pkg/wrapper"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/tidwall/gjson"
)

const (
	PLUGIN_NAME                 = "ai-cache"
	CACHE_KEY_CONTEXT_KEY       = "cacheKey"
	CACHE_CONTENT_CONTEXT_KEY   = "cacheContent"
	PARTIAL_MESSAGE_CONTEXT_KEY = "partialMessage"
	TOOL_CALLS_CONTEXT_KEY      = "toolCalls"
	STREAM_CONTEXT_KEY          = "stream"
	QUERY_EMBEDDING_KEY         = "queryEmbedding"
)

func main() {
	// CreateClient()

	wrapper.SetCtx(
		PLUGIN_NAME,
		wrapper.ParseConfigBy(parseConfig),
		wrapper.ProcessRequestHeadersBy(onHttpRequestHeaders),
		wrapper.ProcessRequestBodyBy(onHttpRequestBody),
		wrapper.ProcessResponseHeadersBy(onHttpResponseHeaders),
		wrapper.ProcessStreamingResponseBodyBy(onHttpResponseBody),
	)
}

func parseConfig(json gjson.Result, config *config.PluginConfig, log wrapper.Log) error {
	// config.EmbeddingProviderConfig.FromJson(json.Get("embeddingProvider"))
	// config.VectorDatabaseProviderConfig.FromJson(json.Get("vectorBaseProvider"))
	// config.RedisConfig.FromJson(json.Get("redis"))
	config.FromJson(json)
	if err := config.Validate(); err != nil {
		return err
	}
	// 注意，在 parseConfig 阶段初始化 client 会出错，比如 docker compose 中的 redis 就无法使用
	if err := config.Complete(log); err != nil {
		log.Errorf("complete config failed:%v", err)
		return err
	}
	return nil
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config config.PluginConfig, log wrapper.Log) types.Action {
	// 这段代码是为了测试，在 parseConfig 阶段初始化 client 会出错，比如 docker compose 中的 redis 就无法使用
	// 但是在 onHttpRequestHeaders 中可以连接到 redis、
	// 修复需要修改 envoy
	// ----------------------------------------------------------------------------
	// if err := config.Complete(log); err != nil {
	// 	log.Errorf("complete config failed:%v", err)
	// }
	// activeCacheProvider := config.GetCacheProvider()
	// if err := activeCacheProvider.Init("", "", 2000); err != nil {
	// 	log.Errorf("init redis failed:%v", err)
	// }
	// activeCacheProvider.Set("test", "test", func(response resp.Value) {})
	// log.Warnf("redis init success")
	// ----------------------------------------------------------------------------

	contentType, _ := proxywasm.GetHttpRequestHeader("content-type")
	// The request does not have a body.
	if contentType == "" {
		return types.ActionContinue
	}
	if !strings.Contains(contentType, "application/json") {
		log.Warnf("content is not json, can't process:%s", contentType)
		ctx.DontReadRequestBody()
		return types.ActionContinue
	}
	proxywasm.RemoveHttpRequestHeader("Accept-Encoding")
	// The request has a body and requires delaying the header transmission until a cache miss occurs,
	// at which point the header should be sent.
	return types.HeaderStopIteration
}

func onHttpRequestBody(ctx wrapper.HttpContext, config config.PluginConfig, body []byte, log wrapper.Log) types.Action {

	bodyJson := gjson.ParseBytes(body)
	// TODO: It may be necessary to support stream mode determination for different LLM providers.
	stream := false
	if bodyJson.Get("stream").Bool() {
		stream = true
		ctx.SetContext(STREAM_CONTEXT_KEY, struct{}{})
	} else if ctx.GetContext(STREAM_CONTEXT_KEY) != nil {
		stream = true
	}
	// key := TrimQuote(bodyJson.Get(config.CacheKeyFrom.RequestBody).Raw)
	key := bodyJson.Get(config.CacheKeyFrom.RequestBody).String()
	ctx.SetContext(CACHE_KEY_CONTEXT_KEY, key)
	log.Debugf("[onHttpRequestBody] key:%s", key)
	if key == "" {
		log.Debug("[onHttpRquestBody] parse key from request body failed")
		return types.ActionContinue
	}

	RedisSearchHandler(key, ctx, config, log, stream, true)

	return types.ActionPause
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config config.PluginConfig, log wrapper.Log) types.Action {
	contentType, _ := proxywasm.GetHttpResponseHeader("content-type")
	if strings.Contains(contentType, "text/event-stream") {
		ctx.SetContext(STREAM_CONTEXT_KEY, struct{}{})
	}
	return types.ActionContinue
}

func onHttpResponseBody(ctx wrapper.HttpContext, config config.PluginConfig, chunk []byte, isLastChunk bool, log wrapper.Log) []byte {
	log.Debugf("[onHttpResponseBody] chunk:%s", string(chunk))
	log.Debugf("[onHttpResponseBody] isLastChunk:%v", isLastChunk)
	if ctx.GetContext(TOOL_CALLS_CONTEXT_KEY) != nil {
		// we should not cache tool call result
		return chunk
	}
	keyI := ctx.GetContext(CACHE_KEY_CONTEXT_KEY)
	if keyI == nil {
		return chunk
	}
	if !isLastChunk {
		stream := ctx.GetContext(STREAM_CONTEXT_KEY)
		if stream == nil {
			tempContentI := ctx.GetContext(CACHE_CONTENT_CONTEXT_KEY)
			if tempContentI == nil {
				ctx.SetContext(CACHE_CONTENT_CONTEXT_KEY, chunk)
				return chunk
			}
			tempContent := tempContentI.([]byte)
			tempContent = append(tempContent, chunk...)
			ctx.SetContext(CACHE_CONTENT_CONTEXT_KEY, tempContent)
		} else {
			var partialMessage []byte
			partialMessageI := ctx.GetContext(PARTIAL_MESSAGE_CONTEXT_KEY)
			if partialMessageI != nil {
				partialMessage = append(partialMessageI.([]byte), chunk...)
			} else {
				partialMessage = chunk
			}
			messages := strings.Split(string(partialMessage), "\n\n")
			for i, msg := range messages {
				if i < len(messages)-1 {
					// process complete message
					processSSEMessage(ctx, config, msg, log)
				}
			}
			if !strings.HasSuffix(string(partialMessage), "\n\n") {
				ctx.SetContext(PARTIAL_MESSAGE_CONTEXT_KEY, []byte(messages[len(messages)-1]))
			} else {
				ctx.SetContext(PARTIAL_MESSAGE_CONTEXT_KEY, nil)
			}
		}
		return chunk
	}
	// last chunk
	key := keyI.(string)
	stream := ctx.GetContext(STREAM_CONTEXT_KEY)
	var value string
	if stream == nil {
		var body []byte
		tempContentI := ctx.GetContext(CACHE_CONTENT_CONTEXT_KEY)
		if tempContentI != nil {
			body = append(tempContentI.([]byte), chunk...)
		} else {
			body = chunk
		}
		bodyJson := gjson.ParseBytes(body)

		value = TrimQuote(bodyJson.Get(config.CacheValueFrom.ResponseBody).Raw)
		if value == "" {
			log.Warnf("parse value from response body failded, body:%s", body)
			return chunk
		}
	} else {
		log.Infof("[onHttpResponseBody] stream mode")
		if len(chunk) > 0 {
			var lastMessage []byte
			partialMessageI := ctx.GetContext(PARTIAL_MESSAGE_CONTEXT_KEY)
			if partialMessageI != nil {
				lastMessage = append(partialMessageI.([]byte), chunk...)
			} else {
				lastMessage = chunk
			}
			if !strings.HasSuffix(string(lastMessage), "\n\n") {
				log.Warnf("[onHttpResponseBody] invalid lastMessage:%s", lastMessage)
				return chunk
			}
			// remove the last \n\n
			lastMessage = lastMessage[:len(lastMessage)-2]
			value = processSSEMessage(ctx, config, string(lastMessage), log)
		} else {
			tempContentI := ctx.GetContext(CACHE_CONTENT_CONTEXT_KEY)
			if tempContentI == nil {
				log.Warnf("[onHttpResponseBody] no content in tempContentI")
				return chunk
			}
			value = tempContentI.(string)
		}
	}
	log.Infof("[onHttpResponseBody] Setting cache to redis, key:%s, value:%s", key, value)
	activeCacheProvider := config.GetCacheProvider()
	config.GetCacheProvider().Set(activeCacheProvider.GetCacheKeyPrefix()+":"+key, value, nil)
	return chunk
}
