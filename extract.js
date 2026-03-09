(function() {
  'use strict';
  
  var out = [];
  var seen = {};
  var count = 0;
  var maxResults = 100; // 最多提取100条
  
  // 多选择器策略：尝试不同的选择器
  var selectors = [
    '#search',
    '#rso',
    '#main',
    'body'
  ];
  
  var root = null;
  for (var i = 0; i < selectors.length; i++) {
    root = document.querySelector(selectors[i]);
    if (root) break;
  }
  
  if (!root) {
    return JSON.stringify({
      error: 'Could not find search results container',
      selectors: selectors
    });
  }
  
  // 尝试多种链接选择器
  var linkSelectors = [
    'a[href^="http"]',
    'a[data-ved]',
    'a[ping]',
    'a[href^="/url"]'
  ];
  
  var links = [];
  for (var j = 0; j < linkSelectors.length; j++) {
    links = root.querySelectorAll(linkSelectors[j]);
    if (links.length > 0) break;
  }
  
  // 处理每个链接
  links.forEach(function(a) {
    if (count >= maxResults) return;
    
    var h = a.href || '';
    
    // 跳过无效链接
    if (!h || h.length < 10) return;
    
    // 过滤 text fragment 链接
    if (/#:~:text=/.test(h)) return;
    
    // 处理 Google 重定向链接
    if (/google\.com\//.test(h)) {
      if (!/google\.com\/url\?/.test(h)) return;
      var m = h.match(/[?&]url=([^&]+)/);
      if (m) {
        try {
          h = decodeURIComponent(m[1]);
        } catch (e) {
          h = m[1];
        }
      }
    }
    
    // 再次过滤（解码后可能包含google链接）
    if (/google\.com\/url\?/.test(h)) {
      var m2 = h.match(/[?&]url=([^&]+)/);
      if (m2) {
        try {
          h = decodeURIComponent(m2[1]);
        } catch (e) {
          h = m2[1];
        }
      }
    }
    
    // 跳过Google内部链接
    if (/^https?:\/\/www\.google\./.test(h) && !/google\.com\/url/.test(h)) {
      return;
    }
    
    // 获取标题：尝试多种策略
    var t = '';
    
    // 策略1：h3标签
    var h3 = a.querySelector('h3');
    if (h3 && h3.innerText) {
      t = h3.innerText.trim();
    }
    
    // 策略2：data属性
    if (!t && a.dataset && a.dataset.ved) {
      var parent = a.closest('div, article, section');
      if (parent) {
        var parentH3 = parent.querySelector('h3');
        if (parentH3 && parentH3.innerText) {
          t = parentH3.innerText.trim();
        }
      }
    }
    
    // 策略3：链接文本
    if (!t && a.innerText) {
      t = a.innerText.trim().slice(0, 200);
    }
    
    // 过滤无效标题
    if (!t || t.length < 2) return;
    
    // 过滤广告类标题
    var lowerTitle = t.toLowerCase();
    var adKeywords = ['ad', 'sponsored', '广告', '赞助'];
    for (var k = 0; k < adKeywords.length; k++) {
      if (lowerTitle === adKeywords[k] || lowerTitle.indexOf(adKeywords[k] + ' ') === 0) {
        return;
      }
    }
    
    // 去重
    if (seen[h]) return;
    seen[h] = true;
    
    out.push({
      title: t,
      url: h
    });
    count++;
  });
  
  // 返回结果，包含元信息
  return JSON.stringify({
    success: true,
    count: count,
    selector: selectors[i],
    linkSelector: linkSelectors[j],
    results: out
  });
})()
