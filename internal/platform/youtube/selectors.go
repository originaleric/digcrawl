package youtube

const (
	DefaultWaitSelector    = "ytd-watch-flexy, ytd-watch-metadata, #above-the-fold, #player"
	DefaultContentSelector = "ytd-watch-metadata, #above-the-fold, #description, body"
)

// MetadataScript extracts public, page-owned data without exposing signed
// playback URLs. Caption tracks are read separately within the Go-side time budget.
func MetadataScript() string {
	return `async () => {
  const player = window.ytInitialPlayerResponse || {};
  const details = player.videoDetails || {};
  const micro = player.microformat?.playerMicroformatRenderer || {};
  const status = player.playabilityStatus || {};
  const textOf = (sel) => document.querySelector(sel)?.textContent?.trim() || '';
  const meta = (name) => document.querySelector('meta[name="' + name + '"], meta[property="' + name + '"]')?.content?.trim() || '';
  const reason = status.reason || '';
  const lowerReason = reason.toLowerCase();
  if (status.status === 'LOGIN_REQUIRED' || lowerReason.includes('sign in')) return {error_code: 'LOGIN_REQUIRED', error_message: reason || 'YouTube requires login', extraction_source: 'page_state'};
  if (status.status === 'UNPLAYABLE' && (lowerReason.includes('country') || lowerReason.includes('region'))) return {error_code: 'REGION_RESTRICTED', error_message: reason || 'YouTube video is region restricted', extraction_source: 'page_state'};
  if (status.status === 'UNPLAYABLE' && lowerReason.includes('private')) return {error_code: 'CONTENT_PRIVATE', error_message: reason || 'YouTube video is private', extraction_source: 'page_state'};
  if (status.status === 'UNPLAYABLE') return {error_code: 'CONTENT_NOT_FOUND', error_message: reason || 'YouTube video is unavailable', extraction_source: 'page_state'};
  if (status.status === 'ERROR') return {error_code: 'CONTENT_NOT_FOUND', error_message: status.reason || 'YouTube video was not found', extraction_source: 'page_state'};
  const captionTracks = player.captions?.playerCaptionsTracklistRenderer?.captionTracks || [];
  const captions = captionTracks.map(track => ({
    base_url: track.baseUrl || '', language_code: track.languageCode || '',
    name: track.name?.simpleText || track.name?.runs?.map(run => run.text).join('') || '',
    kind: track.kind || ''
  })).filter(track => track.base_url);
  const title = details.title || micro.title?.simpleText || meta('og:title') || textOf('h1') || document.title || '';
  const description = details.shortDescription || micro.description?.simpleText || meta('og:description') || textOf('#description-inline-expander') || '';
  const author = details.author || micro.ownerChannelName || meta('author') || '';
  const thumbnail = details.thumbnail?.thumbnails?.slice(-1)[0]?.url || meta('og:image') || '';
  return {
    title, description, video_id: details.videoId || '', author_name: author, author_id: details.channelId || '',
    author_profile_url: details.channelId ? 'https://www.youtube.com/channel/' + details.channelId : '',
    published_at: micro.publishDate || micro.uploadDate || meta('datePublished') || '',
    cover_url: thumbnail, caption_tracks: captions,
    extraction_source: details.videoId ? 'player_response' : 'browser'
  };
}`
}
